package builtin

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebFetchMalformedProxyDoesNotRevealCredentials(t *testing.T) {
	transport := ssrfGuardedTransport("https://user:TEST_PROXY_SECRET@127.0.0.1:%invalid")
	conn, err := transport.DialContext(t.Context(), "tcp", "public.invalid:443")
	if conn != nil || !errors.Is(err, errInvalidFetchProxy) || strings.Contains(err.Error(), "TEST_PROXY_SECRET") {
		t.Fatalf("malformed proxy: connection=%v error=%v", conn, err)
	}
}

func TestWebFetchHTTPSProxyNeverSendsPlaintextCONNECT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	first := make(chan byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		var b [1]byte
		if _, err := io.ReadFull(conn, b[:]); err == nil {
			first <- b[0]
		}
	}()
	tr := ssrfGuardedTransport("https://dummy-user:dummy-password@" + ln.Addr().String())
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	resp, err := client.Get("http://public.example.invalid/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("plaintext listener was accepted as an HTTPS proxy")
	}
	select {
	case b := <-first:
		if b != 0x16 {
			t.Fatalf("first proxy byte=0x%x, want TLS handshake (0x16)", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not receive a TLS handshake")
	}
}

func TestWebFetchHTTPSProxyUsesVerifiedTLS(t *testing.T) {
	if proxyURL := os.Getenv("REASONIX_TEST_HTTPS_PROXY"); proxyURL != "" {
		tr := ssrfGuardedTransport(proxyURL)
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
		_, err := client.Get("http://public.example.invalid/")
		if err == nil {
			t.Fatal("test proxy must refuse the tunnel")
		}
		if os.Getenv("REASONIX_TEST_PROXY_TRUSTED") == "true" && !strings.Contains(err.Error(), "502") {
			t.Fatalf("trusted TLS proxy was not reached: %v", err)
		}
		return
	}
	var hits atomic.Int32
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.TLS == nil || r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != "Basic ZHVtbXktdXNlcjpkdW1teS1wYXNzd29yZA==" {
			t.Errorf("proxy request TLS=%v method=%s auth=%q", r.TLS != nil, r.Method, r.Header.Get("Proxy-Authorization"))
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	certPath := filepath.Join(t.TempDir(), "proxy-cert.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, trusted := range []bool{true, false} {
		t.Run(map[bool]string{true: "trusted", false: "untrusted"}[trusted], func(t *testing.T) {
			root := certPath
			if !trusted {
				root = filepath.Join(t.TempDir(), "missing.pem")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWebFetchHTTPSProxyUsesVerifiedTLS$")
			cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+root, "SSL_CERT_DIR="+t.TempDir(), "REASONIX_TEST_HTTPS_PROXY=https://dummy-user:dummy-password@"+strings.TrimPrefix(proxy.URL, "https://"), "REASONIX_TEST_PROXY_TRUSTED="+map[bool]string{true: "true", false: "false"}[trusted])
			before := hits.Load()
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("TLS subprocess: %v\n%s", err, out)
			}
			want := int32(0)
			if trusted {
				want = 1
			}
			if got := hits.Load() - before; got != want {
				t.Fatalf("authenticated CONNECT requests=%d, want %d", got, want)
			}
		})
	}
}
