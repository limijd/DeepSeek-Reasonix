package installsource

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallProxyCannotReachInternalTargets(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "unexpected proxy access")
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := ssrfGuardClient(&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: time.Second})
	for _, scheme := range []string{"http", "https"} {
		for _, ip := range []string{"169.254.169.254", "10.0.0.1", "100.100.100.200", "[fc00::1]"} {
			resp, err := client.Get(scheme + "://" + ip + "/metadata")
			if resp != nil {
				_ = resp.Body.Close()
			}
			if err == nil {
				t.Fatalf("%s://%s passed the SSRF guard", scheme, ip)
			}
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("blocked targets reached the proxy %d times", hits.Load())
	}
}

func stubInstallLookup(t *testing.T, ips ...string) {
	t.Helper()
	old := lookupInstallHost
	t.Cleanup(func() { lookupInstallHost = old })
	lookupInstallHost = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if ip := net.ParseIP(host); ip != nil {
			return []net.IPAddr{{IP: ip}}, nil
		}
		var result []net.IPAddr
		for _, ip := range ips {
			result = append(result, net.IPAddr{IP: net.ParseIP(ip)})
		}
		return result, nil
	}
}

func TestInstallProxyRefusesNamesResolvingToInternalAddresses(t *testing.T) {
	stubInstallLookup(t, "169.254.169.254")
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "internal response")
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := ssrfGuardClient(&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: time.Second})
	resp, err := client.Get("http://metadata.example.invalid/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || hits.Load() != 0 {
		t.Fatalf("internal hostname: error=%v proxy hits=%d", err, hits.Load())
	}
}

func TestInstallProxyPinsDNSAndGuardsRedirects(t *testing.T) {
	stubInstallLookup(t, "127.0.0.1")
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		if r.Method != http.MethodConnect || r.Host != "127.0.0.1:80" {
			t.Errorf("proxy received %s %s, want CONNECT 127.0.0.1:80", r.Method, r.Host)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		req, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		defer req.Body.Close()
		if req.Host != "install.example.invalid" {
			t.Errorf("HTTP Host=%q, want original hostname", req.Host)
		}
		if req.URL.Path == "/redirect" {
			_, _ = rw.WriteString("HTTP/1.1 302 Found\r\nLocation: http://169.254.169.254/metadata\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		} else {
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len("public bytes"), "public bytes")
		}
		_ = rw.Flush()
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := ssrfGuardClient(&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 2 * time.Second})
	resp, err := client.Get("http://install.example.invalid/file")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "public bytes" {
		t.Fatalf("body=%q error=%v", body, err)
	}
	resp, err = client.Get("http://install.example.invalid/redirect")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "169.254.169.254") || connects.Load() != 2 {
		t.Fatalf("redirect: error=%v proxy connects=%d", err, connects.Load())
	}
}

func TestInstallProxyKeepsTargetTLSVerification(t *testing.T) {
	stubInstallLookup(t, "127.0.0.1")
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		if r.TLS.ServerName != "example.com" || !strings.HasPrefix(r.Host, "example.com:") {
			t.Errorf("target SNI=%q Host=%q", r.TLS.ServerName, r.Host)
		}
		_, _ = io.WriteString(w, "verified target")
	}))
	defer target.Close()
	targetAddr := strings.TrimPrefix(target.URL, "https://")
	_, port, _ := net.SplitHostPort(targetAddr)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != targetAddr {
			t.Errorf("proxy target=%s %s, want CONNECT %s", r.Method, r.Host, targetAddr)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		upstream, err := net.DialTimeout("tcp", targetAddr, time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		defer upstream.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_ = upstream.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		go func() { _, _ = io.Copy(upstream, rw) }()
		_, _ = io.Copy(conn, upstream)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	base := target.Client().Transport.(*http.Transport).Clone()
	if base.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS fixture must verify certificates")
	}
	base.Proxy = http.ProxyURL(pu)
	client := ssrfGuardClient(&http.Client{Transport: base, Timeout: 3 * time.Second})
	resp, err := client.Get("https://example.com:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "verified target" {
		t.Fatalf("body=%q error=%v", body, err)
	}
	resp, err = client.Get("https://wrong-host.example:" + port + "/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || targetHits.Load() != 1 {
		t.Fatalf("invalid target certificate accepted: error=%v requests=%d", err, targetHits.Load())
	}
}
