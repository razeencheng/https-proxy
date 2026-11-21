package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 定义缓冲区与隧道收尾常量，兼顾吞吐性能与长时运行的内存稳定性
const (
	// 默认缓冲区大小为64KB（网络I/O最佳实践）
	DefaultBufferSize = 64 * 1024
	// 单方向转发缓冲区上限（128KB；TLS记录最大明文仅16KB，过大的buffer_size会导致高并发下大量堆分配与GC停顿）
	MaxCopyBufferSize = 128 * 1024
	// 单方向流结束后的半关闭宽限时间，超时立即强制双向关闭防止僵尸连接与Goroutine泄漏
	defaultHalfCloseTimeout = 15 * time.Second
)

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, MaxCopyBufferSize)
		return &buf
	},
}

// Proxy represents the HTTPS proxy server
type Proxy struct {
	Config         *Config
	CACertPool     *x509.CertPool  // Certificate Authority certificate pool
	StatsManager   *StatsManager   // Legacy statistics manager
	StatsCollector *StatsCollector // New async stats collector
	StatsDB        *StatsDB        // SQLite stats database
	GeoIP          *GeoIPService   // GeoIP lookup service
}

// GzipResponseWriter 提供按需gzip压缩支持，避免对已压缩或无响应体的响应重复压缩
type GzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	shouldGzip  bool
	isHead      bool
}

func headerContainsToken(h http.Header, key, token string) bool {
	for _, v := range h.Values(key) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func statusAllowsBody(statusCode int) bool {
	return statusCode >= http.StatusOK &&
		statusCode != http.StatusNoContent &&
		statusCode != http.StatusNotModified
}

// WriteHeader 在首次写入响应头时决定是否启用gzip压缩
func (w *GzipResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	if !w.isHead &&
		statusAllowsBody(statusCode) &&
		w.Header().Get("Content-Encoding") == "" &&
		w.Header().Get("Upgrade") == "" {
		w.shouldGzip = true
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		if !headerContainsToken(w.Header(), "Vary", "Accept-Encoding") {
			w.Header().Add("Vary", "Accept-Encoding")
		}
		w.gz = gzip.NewWriter(w.ResponseWriter)
	}

	w.ResponseWriter.WriteHeader(statusCode)
}

// Write 使用gzip压缩写入数据（若未压缩则直接透传）
func (w *GzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.shouldGzip {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush 刷新gzip缓冲区和底层ResponseWriter
func (w *GzipResponseWriter) Flush() {
	if w.shouldGzip && w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传底层http.Hijacker能力
func (w *GzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacking not supported")
}

// Unwrap 返回底层http.ResponseWriter供http.ResponseController使用
func (w *GzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Close 关闭gzip写入器
func (w *GzipResponseWriter) Close() error {
	if w.gz != nil {
		return w.gz.Close()
	}
	return nil
}

// NewGzipResponseWriter 创建一个新的按需gzip响应写入器
func NewGzipResponseWriter(w http.ResponseWriter) *GzipResponseWriter {
	return &GzipResponseWriter{
		ResponseWriter: w,
	}
}

func wrapCompressionHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect || !headerContainsToken(r.Header, "Accept-Encoding", "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gzw := NewGzipResponseWriter(w)
		gzw.isHead = r.Method == http.MethodHead
		defer gzw.Close()
		next.ServeHTTP(gzw, r)
	})
}

func main() {
	// Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair(cfg.Server.Certificates.CertPath, cfg.Server.Certificates.KeyPath)
	if err != nil {
		log.Fatalf("failed to load server certificate and key: %v", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(cfg.Server.Certificates.CAPath)
	if err != nil {
		log.Fatalf("failed to load CA certificate: %v", err)
	}

	// Create a CA certificate pool and add the CA certificate
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		log.Fatalf("failed to parse CA certificate")
	}

	// Create statistics manager (legacy, kept for compatibility)
	statsManager := NewStatsManager(cfg)

	// Initialize new SQLite-based stats
	var statsDB *StatsDB
	var statsCollector *StatsCollector
	var geoIP *GeoIPService

	if cfg.Stats.Enabled {
		var err2 error
		statsDB, err2 = NewStatsDB(cfg.Stats.DBPath)
		if err2 != nil {
			log.Fatalf("failed to init stats database: %v", err2)
		}
		log.Printf("Stats database opened: %s", cfg.Stats.DBPath)

		// Initialize GeoIP
		if cfg.GeoIP.Enabled {
			geoIP = NewGeoIPService(cfg.GeoIP.DBPath)
		}

		// Create async collector
		statsCollector = NewStatsCollector(statsDB, geoIP, cfg.Stats.FlushInterval)

		// Migrate from legacy JSON if it exists
		if cfg.Stats.FilePath != "" {
			if legacyStats := statsManager.GetUserStats(); len(legacyStats) > 0 {
				if err := statsDB.MigrateFromJSON(legacyStats); err != nil {
					log.Printf("Warning: JSON migration failed: %v", err)
				} else {
					log.Printf("Migrated %d users from legacy JSON stats", len(legacyStats))
				}
			}
		}

		// Start periodic cleanup
		go func() {
			ticker := time.NewTicker(6 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				statsDB.CleanupOldData(cfg.Stats.Retention.MinuteStatsDays, cfg.Stats.Retention.HourlyStatsDays)
			}
		}()
	}

	// Create admin panel server
	adminServer, err := NewAdminServer(cfg, statsManager, statsDB)
	if err != nil {
		log.Printf("Warning: Failed to create admin server: %v", err)
	}

	// Create a proxy with the configuration
	prx := &Proxy{
		Config:         cfg,
		CACertPool:     caCertPool,
		StatsManager:   statsManager,
		StatsCollector: statsCollector,
		StatsDB:        statsDB,
		GeoIP:          geoIP,
	}

	// Create an HTTPS server with the TLS config
	server := &http.Server{
		Addr: ":" + strconv.Itoa(cfg.Server.Port),
		TLSConfig: &tls.Config{
			Certificates:          []tls.Certificate{serverCert},
			ClientCAs:             caCertPool,
			ClientAuth:            tls.RequestClientCert, // Request but do not require client certificates
			NextProtos:            []string{"http/1.1"},
			VerifyPeerCertificate: nil, // We verify certificates ourselves in ServeHTTP
		},
		Handler: prx,
		// 优化HTTP服务器配置
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // 更长的写超时
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// 如果配置中启用了压缩，则添加压缩中间件
	if cfg.Server.Performance.EnableCompression {
		server.Handler = wrapCompressionHandler(prx)
	}

	// Start the admin panel server (if configured)
	if adminServer != nil {
		adminServer.Start()
	}

	// Set up graceful shutdown
	setupGracefulShutdown(server, statsManager, adminServer, statsCollector, statsDB, geoIP)

	// Start the HTTPS server
	log.Printf("Starting HTTPS server on port %d...\n", cfg.Server.Port)
	err = server.ListenAndServeTLS("", "")
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to start HTTPS server: %v", err)
	}
}

// setupGracefulShutdown sets up graceful shutdown to ensure statistics are saved
func setupGracefulShutdown(server *http.Server, statsManager *StatsManager, adminServer *AdminServer, statsCollector *StatsCollector, statsDB *StatsDB, geoIP *GeoIPService) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("Shutting down server...")

		// Close HTTP server
		if err := server.Close(); err != nil {
			log.Printf("Error closing server: %v", err)
		}

		// Stop admin panel server
		if adminServer != nil {
			adminServer.Stop()
		}

		// Stop new stats collector (flushes remaining data)
		if statsCollector != nil {
			statsCollector.Stop()
		}

		// Close stats database
		if statsDB != nil {
			statsDB.Close()
		}

		// Close GeoIP
		if geoIP != nil {
			geoIP.Close()
		}

		// Stop legacy statistics manager and save data
		statsManager.Stop()

		log.Println("Server shutdown complete")
		os.Exit(0)
	}()
}

// verifyClientCert verifies if the client certificate is issued by a trusted CA
func (p *Proxy) verifyClientCert(cert *x509.Certificate) bool {
	// Create verification options
	opts := x509.VerifyOptions{
		Roots:     p.CACertPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Verify certificate chain
	_, err := cert.Verify(opts)
	if err != nil {
		log.Printf("Certificate verification failed: %v", err)
		return false
	}

	// Certificate verification passed
	return true
}

// getUsernameFromCert extracts the username from the certificate
func getUsernameFromCert(cert *x509.Certificate) string {
	// Use the certificate's Common Name as the user identifier
	return cert.Subject.CommonName
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if the client provided a certificate
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		log.Println("No client certificate provided")
		fmt.Println("Unauthorized request (no certificate): ", r.Method, r.RequestURI, r.RemoteAddr)

		if r.Method == http.MethodConnect {
			http.Error(w, "Client certificate required", http.StatusMethodNotAllowed)
			return
		}

		p.proxyUnauthorizedRequest(w, r)
		return
	}

	// Get client certificate
	clientCert := r.TLS.PeerCertificates[0]

	// Get username
	username := getUsernameFromCert(clientCert)

	// Verify client certificate
	isValid := p.verifyClientCert(clientCert)

	// Check if user is disabled (check new DB first, then legacy)
	if isValid {
		disabled := false
		if p.StatsDB != nil {
			disabled = p.StatsDB.IsUserDisabled(username)
		} else {
			disabled = p.StatsManager.IsUserDisabled(username)
		}
		if disabled {
			log.Printf("Disabled user rejected: %s, CN: %s", r.RemoteAddr, username)
			http.Error(w, "Access denied: Your account has been disabled", http.StatusForbidden)
			return
		}
	}

	if r.Method == http.MethodConnect {
		if isValid {
			// Record connection
			p.StatsManager.RecordConnection(username)

			log.Printf("Authorized client: %s, CN: %s", r.RemoteAddr, clientCert.Subject.CommonName)
			fmt.Printf("Authorized request: %s %s %s\n", r.Method, r.RequestURI, r.RemoteAddr)

			// Handle connection and track traffic
			p.handleConnectWithStats(w, r, username)
			return
		} else {
			log.Printf("Unauthorized client: %s, CN: %s", r.RemoteAddr, clientCert.Subject.CommonName)
			http.Error(w, "Invalid client certificate", http.StatusMethodNotAllowed)
			return
		}
	}

	// Record request
	if isValid {
		p.StatsManager.RecordRequest(username)
		if p.StatsDB != nil {
			p.StatsDB.IncrementRequestCount(username)
		}
	}

	fmt.Println("Unauthorized request: ", r.Method, r.RequestURI, r.RemoteAddr)
	p.proxyUnauthorizedRequest(w, r)
}

func (p *Proxy) proxyUnauthorizedRequest(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(p.Config.Proxy.DefaultSite)
	if err != nil || target.Scheme == "" || target.Host == "" {
		http.Error(w, "Page not found", http.StatusNotFound)
		log.Printf("Invalid default_site %q: %v", p.Config.Proxy.DefaultSite, err)
		return
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("proxyUnauthorizedRequest error: %v", err)
			http.Error(rw, "Page not found", http.StatusNotFound)
		},
	}
	rp.ServeHTTP(w, r)
}

// 获取配置的缓冲区大小（封顶 MaxCopyBufferSize，避免配置过大导致高并发下内存暴涨）
func (p *Proxy) getBufferSize() int {
	if p.Config != nil && p.Config.Server.Performance.BufferSize > 0 {
		if p.Config.Server.Performance.BufferSize > MaxCopyBufferSize {
			return MaxCopyBufferSize
		}
		return p.Config.Server.Performance.BufferSize
	}
	return DefaultBufferSize
}

// 获取TCP Keep Alive时间
func (p *Proxy) getTCPKeepAlive() time.Duration {
	if p.Config.Server.Performance.TCPKeepAlive > 0 {
		return time.Duration(p.Config.Server.Performance.TCPKeepAlive) * time.Second
	}
	return 30 * time.Second
}

// 获取是否禁用Nagle算法
func (p *Proxy) getNoDelay() bool {
	return p.Config.Server.Performance.NoDelay
}

type closeWriter interface {
	CloseWrite() error
}

// extractTCPConn 提取底层 *net.TCPConn（兼容直接 TCP 连接与 *tls.Conn 包装连接）
func extractTCPConn(c net.Conn) *net.TCPConn {
	if tc, ok := c.(*net.TCPConn); ok {
		return tc
	}
	if tlsConn, ok := c.(*tls.Conn); ok {
		if tc, ok := tlsConn.NetConn().(*net.TCPConn); ok {
			return tc
		}
	}
	return nil
}

// closeWriteConn 尝试优雅半关闭连接写端（若为 *tls.Conn 则先发 close_notify 再发底层 TCP FIN）
func closeWriteConn(c net.Conn) bool {
	didHalfClose := false
	if cw, ok := c.(closeWriter); ok {
		if cw.CloseWrite() == nil {
			didHalfClose = true
		}
	}
	if tlsConn, ok := c.(*tls.Conn); ok {
		if tc, ok := tlsConn.NetConn().(*net.TCPConn); ok {
			if tc.CloseWrite() == nil {
				didHalfClose = true
			}
		}
	}
	return didHalfClose
}

func (p *Proxy) applyTCPConnTuning(c net.Conn) {
	tcpConn := extractTCPConn(c)
	if tcpConn == nil {
		return
	}
	_ = tcpConn.SetNoDelay(p.getNoDelay())
	_ = tcpConn.SetKeepAlive(true)
	_ = tcpConn.SetKeepAlivePeriod(p.getTCPKeepAlive())
	// 注意：不调用 SetReadBuffer / SetWriteBuffer，避免 Linux 内核关闭 TCP 动态缓冲自动调优
	// （setsockopt SO_RCVBUF/SO_SNDBUF 会设置 SOCK_RCVBUF_LOCK/SOCK_SNDBUF_LOCK，
	// 导致高延迟跨洋链路的发送窗口被锁死而无法随 tcp_rmem/tcp_wmem 扩展）。
}

// relayTunnel 双向转发客户端与目标端流量，复用池化缓冲区并消除单向断开时的死锁与连接泄漏
func relayTunnel(clientConn net.Conn, clientInitReader io.Reader, serverConn net.Conn, bufSize int, halfCloseTimeout time.Duration) (uint64, uint64) {
	if bufSize <= 0 {
		bufSize = DefaultBufferSize
	} else if bufSize > MaxCopyBufferSize {
		bufSize = MaxCopyBufferSize
	}
	if halfCloseTimeout <= 0 {
		halfCloseTimeout = defaultHalfCloseTimeout
	}

	var clientSrc io.Reader = clientConn
	if clientInitReader != nil {
		clientSrc = io.MultiReader(clientInitReader, clientConn)
	}

	clientReader := NewCountingReader(clientSrc)
	clientWriter := NewCountingWriter(clientConn)
	serverReader := NewCountingReader(serverConn)
	serverWriter := NewCountingWriter(serverConn)

	upBufPtr := copyBufPool.Get().(*[]byte)
	downBufPtr := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(upBufPtr)
	defer copyBufPool.Put(downBufPtr)

	uploadBuf := (*upBufPtr)[:bufSize]
	downloadBuf := (*downBufPtr)[:bufSize]

	done := make(chan struct{})
	go func() {
		_, err := io.CopyBuffer(serverWriter, clientReader, uploadBuf)
		if err != nil {
			_ = serverConn.Close()
			_ = clientConn.Close()
		} else {
			if !closeWriteConn(serverConn) {
				_ = serverConn.Close()
			} else {
				_ = serverConn.SetReadDeadline(time.Now().Add(halfCloseTimeout))
			}
		}
		close(done)
	}()

	_, downErr := io.CopyBuffer(clientWriter, serverReader, downloadBuf)
	if downErr != nil {
		_ = clientConn.Close()
		_ = serverConn.Close()
	} else {
		if !closeWriteConn(clientConn) {
			_ = clientConn.Close()
		} else {
			_ = clientConn.SetReadDeadline(time.Now().Add(halfCloseTimeout))
		}
	}

	<-done
	_ = clientConn.Close()
	_ = serverConn.Close()

	return clientReader.BytesRead(), serverReader.BytesRead()
}

// handleConnectWithStats handles CONNECT requests and tracks traffic statistics
func (p *Proxy) handleConnectWithStats(w http.ResponseWriter, r *http.Request, username string) {
	// Extract the host and port from the request URI
	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = "443"
	}

	// 创建自定义的TCP连接配置来优化性能
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: p.getTCPKeepAlive(),
		DualStack: true, // 启用IPv4/IPv6双栈
	}

	dialCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 使用自定义配置的连接
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to connect to target host: %v", err), http.StatusBadGateway)
		return
	}
	defer conn.Close()

	// Extract the remote IP for GeoIP lookup
	targetIP := ""
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		targetIP = tcpAddr.IP.String()
	} else if addr := conn.RemoteAddr(); addr != nil {
		if h, _, splitErr := net.SplitHostPort(addr.String()); splitErr == nil {
			targetIP = h
		}
	}

	// 设置出站TCP参数以优化性能
	p.applyTCPConnTuning(conn)

	// Hijack客户端连接并返回200隧道建立响应
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send connection established message
	if _, err := clientConn.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n")); err != nil {
		return
	}

	// 解包 *tls.Conn 应用底层客户端 TCP KeepAlive 与 NoDelay 优化
	p.applyTCPConnTuning(clientConn)

	var bufferedClientData io.Reader
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		bufferedClientData = io.LimitReader(clientBuf.Reader, int64(clientBuf.Reader.Buffered()))
	}

	uploadBytes, downloadBytes := relayTunnel(clientConn, bufferedClientData, conn, p.getBufferSize(), defaultHalfCloseTimeout)

	// Record traffic to legacy StatsManager in one shot (no per-read locking)
	p.StatsManager.RecordTraffic(username, uploadBytes+downloadBytes)

	// Emit TrafficEvent to the new async collector
	if p.StatsCollector != nil {
		p.StatsCollector.Record(TrafficEvent{
			Username:  username,
			Domain:    host,
			TargetIP:  targetIP,
			Upload:    uploadBytes,
			Download:  downloadBytes,
			Timestamp: time.Now(),
		})
	}
}
