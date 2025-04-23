package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

// Proxy represents the HTTPS proxy server
type Proxy struct {
	Config       *Config
	CACertPool   *x509.CertPool // Certificate Authority certificate pool
	StatsManager *StatsManager  // Statistics manager for tracking user activities
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

	// Create statistics manager
	statsManager := NewStatsManager(cfg)

	// Create admin panel server
	adminServer, err := NewAdminServer(cfg, statsManager)
	if err != nil {
		log.Printf("Warning: Failed to create admin server: %v", err)
	}

	// Create a proxy with the configuration
	prx := &Proxy{
		Config:       cfg,
		CACertPool:   caCertPool,
		StatsManager: statsManager,
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
	}

	// Start the admin panel server (if configured)
	if adminServer != nil {
		adminServer.Start()
	}

	// Set up graceful shutdown
	setupGracefulShutdown(server, statsManager, adminServer)

	// Start the HTTPS server
	log.Printf("Starting HTTPS server on port %d...\n", cfg.Server.Port)
	err = server.ListenAndServeTLS("", "")
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to start HTTPS server: %v", err)
	}
}

// setupGracefulShutdown sets up graceful shutdown to ensure statistics are saved
func setupGracefulShutdown(server *http.Server, statsManager *StatsManager, adminServer *AdminServer) {
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

		// Stop statistics manager and save data
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

	// Check if user is disabled
	if isValid && p.StatsManager.IsUserDisabled(username) {
		log.Printf("Disabled user rejected: %s, CN: %s", r.RemoteAddr, username)
		http.Error(w, "Access denied: Your account has been disabled", http.StatusForbidden)
		return
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
	}

	fmt.Println("Unauthorized request: ", r.Method, r.RequestURI, r.RemoteAddr)
	p.proxyUnauthorizedRequest(w, r)
}

func (p *Proxy) proxyUnauthorizedRequest(w http.ResponseWriter, r *http.Request) {
	url := p.Config.Proxy.DefaultSite + r.RequestURI
	req, err := http.NewRequest(r.Method, url, r.Body)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Page not found"))
		fmt.Println("newRequest: ", err)
		return
	}

	// Copy headers from the original request
	for k, v := range r.Header {
		req.Header[k] = v
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("do: ", err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Page not found"))
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	io.Copy(w, resp.Body)
}

// handleConnectWithStats handles CONNECT requests and tracks traffic statistics
func (p *Proxy) handleConnectWithStats(w http.ResponseWriter, r *http.Request, username string) {
	// Extract the host and port from the request URI
	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = "443"
	}

	// Establish a TCP connection to the target host
	conn, err := net.Dial("tcp4", net.JoinHostPort(host, port))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to connect to target host: %v", err), http.StatusBadGateway)
		return
	}
	defer conn.Close()

	// Send a 200 OK response to the client
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send connection established message
	clientConn.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n"))

	// Create readers and writers for statistics tracking
	clientReader := NewCountingReader(clientConn)
	clientWriter := NewCountingWriter(clientConn)
	serverReader := NewCountingReader(conn)
	serverWriter := NewCountingWriter(conn)

	// Create user traffic readers for statistics tracking
	userClientReader := NewUserTrafficReader(p.StatsManager, username, clientReader)
	userServerReader := NewUserTrafficReader(p.StatsManager, username, serverReader)

	// Set up traffic copying from client to server (with statistics)
	go func() {
		io.Copy(serverWriter, userClientReader)
		conn.Close()
	}()

	// Set up traffic copying from server to client (with statistics)
	io.Copy(clientWriter, userServerReader)
}
