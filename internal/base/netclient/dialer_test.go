package netclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDialProxyPreservesPinnedTargetAndCredentials(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "127.0.0.1:12345" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpzZWNyZXQ=" || r.Header.Get("Authorization") != "" {
			t.Errorf("CONNECT method=%s host=%s headers=%v", r.Method, r.Host, r.Header)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\ntunnel bytes")
		_ = rw.Flush()
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	pu.User = url.UserPassword("user", "secret")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	conn, err := DialProxy(ctx, pu, &net.Dialer{Timeout: time.Second}, "tcp", "127.0.0.1:12345")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body, err := io.ReadAll(conn)
	if err != nil || string(body) != "tunnel bytes" {
		t.Fatalf("tunnel body=%q error=%v", body, err)
	}
}

func TestDialProxyRejectsUnsupportedScheme(t *testing.T) {
	conn, err := DialProxy(context.Background(), &url.URL{Scheme: "ftp", Host: "127.0.0.1:1"}, &net.Dialer{}, "tcp", "public.example:80")
	if err == nil || conn != nil {
		t.Fatalf("unsupported proxy scheme silently dialed: conn=%v error=%v", conn, err)
	}
}
