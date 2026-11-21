package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		t.Fatalf("gzip write failed: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}
	return buf.Bytes()
}

func gunzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip NewReader failed: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip ReadAll failed: %v", err)
	}
	return out
}

func TestProxyUnauthorizedRequestWithCompressionMiddleware(t *testing.T) {
	const htmlBody = "<html><body><h1>Hello Proxy</h1></body></html>"

	t.Run("upstream already gzipped is not double-compressed", func(t *testing.T) {
		var seenHost string
		var seenPath string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenHost = r.Host
			seenPath = r.URL.RequestURI()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(gzipBytes(t, []byte(htmlBody)))
		}))
		defer upstream.Close()

		targetURL, _ := url.Parse(upstream.URL)
		prx := &Proxy{
			Config: &Config{
				Proxy: ProxyConfig{DefaultSite: upstream.URL},
			},
		}
		handler := wrapCompressionHandler(prx)

		req := httptest.NewRequest(http.MethodGet, "https://proxy.local/index?foo=bar", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}
		if got := res.Header.Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("expected Content-Encoding 'gzip', got %q", got)
		}
		if seenHost != targetURL.Host {
			t.Fatalf("expected upstream Host %q, got %q", targetURL.Host, seenHost)
		}
		if seenPath != "/index?foo=bar" {
			t.Fatalf("expected upstream path '/index?foo=bar', got %q", seenPath)
		}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read response body failed: %v", err)
		}
		decoded := gunzipBytes(t, body)
		if string(decoded) != htmlBody {
			t.Fatalf("expected decoded body %q, got %q", htmlBody, string(decoded))
		}
	})

	t.Run("upstream brotli response is passed through untouched", func(t *testing.T) {
		fakeBrotli := []byte{0x1b, 0x2f, 0x00, 0x00, 0xfe, 0xaa, 0xbb}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Encoding", "br")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fakeBrotli)
		}))
		defer upstream.Close()

		prx := &Proxy{
			Config: &Config{
				Proxy: ProxyConfig{DefaultSite: upstream.URL},
			},
		}
		handler := wrapCompressionHandler(prx)

		req := httptest.NewRequest(http.MethodGet, "https://proxy.local/", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if got := res.Header.Get("Content-Encoding"); got != "br" {
			t.Fatalf("expected Content-Encoding 'br', got %q", got)
		}
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read response body failed: %v", err)
		}
		if !bytes.Equal(body, fakeBrotli) {
			t.Fatalf("expected raw br bytes %x, got %x", fakeBrotli, body)
		}
	})

	t.Run("upstream uncompressed response is gzip-compressed cleanly", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(htmlBody))
		}))
		defer upstream.Close()

		prx := &Proxy{
			Config: &Config{
				Proxy: ProxyConfig{DefaultSite: upstream.URL},
			},
		}
		handler := wrapCompressionHandler(prx)

		req := httptest.NewRequest(http.MethodGet, "https://proxy.local/", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if got := res.Header.Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("expected Content-Encoding 'gzip', got %q", got)
		}
		if res.Header.Get("Content-Length") != "" {
			t.Fatalf("expected Content-Length to be cleared on compressed response, got %q", res.Header.Get("Content-Length"))
		}
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read response body failed: %v", err)
		}
		decoded := gunzipBytes(t, body)
		if string(decoded) != htmlBody {
			t.Fatalf("expected decoded body %q, got %q", htmlBody, string(decoded))
		}
	})

	t.Run("304 and HEAD responses are not gzip-wrapped", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotModified)
		}))
		defer upstream.Close()

		prx := &Proxy{
			Config: &Config{
				Proxy: ProxyConfig{DefaultSite: upstream.URL},
			},
		}
		handler := wrapCompressionHandler(prx)

		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req := httptest.NewRequest(method, "https://proxy.local/", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatalf("%s read failed: %v", method, err)
			}
			if got := res.Header.Get("Content-Encoding"); got != "" {
				t.Fatalf("%s expected empty Content-Encoding, got %q", method, got)
			}
			if len(body) != 0 {
				t.Fatalf("%s expected empty body, got %d bytes", method, len(body))
			}
		}
	})
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestCompressionMiddlewareSkipsConnectAndPreservesHijacker(t *testing.T) {
	var sawHijacker bool
	handler := wrapCompressionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawHijacker = w.(http.Hijacker)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodConnect, "example.com:443", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.ServeHTTP(rec, req)

	if !sawHijacker {
		t.Fatalf("expected CONNECT handler to retain http.Hijacker")
	}
}
