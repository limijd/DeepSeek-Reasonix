package netpolicy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCleanForkPreservesUserNetworkAndBlocksUpstream(t *testing.T) {
	Enable()
	for _, host := range []string{"api.deepseek.com", "api.openai.com", "github.com", "localhost", "127.0.0.1", "10.0.0.1", "fd00::1", "reasonix.io.example.test"} {
		if !AllowedHost(host) {
			t.Errorf("user network refused: %s", host)
		}
	}
	for _, host := range []string{"reasonix.io", "ID.Reasonix.IO.", "remote.reasonix.io", "crash.reasonix.io", "dl.reasonix.io", "api.typesafe.ai"} {
		if AllowedHost(host) {
			t.Errorf("upstream admitted: %s", host)
		}
	}
	calls := 0
	client := GuardClient(&http.Client{Transport: policyTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: http.Header{}}, nil
	})})
	for _, raw := range []string{"https://api.deepseek.com/models", "https://api.deepseek.com/user/balance", "http://10.0.0.1:8080/", "https://github.com/", "https://api.openai.com/v1/models"} {
		response, err := client.Get(raw)
		if err != nil {
			t.Fatalf("normal GET refused: %s: %v", raw, err)
		}
		_ = response.Body.Close()
	}
	before := calls
	for _, raw := range []string{"https://id.reasonix.io/me", "https://crash.reasonix.io/v1/ping", "https://dl.reasonix.io/studio/index.json"} {
		if _, err := client.Get(raw); !errors.Is(err, ErrBlocked) {
			t.Fatalf("upstream request: %s: %v", raw, err)
		}
	}
	if calls != before {
		t.Fatal("blocked request reached transport")
	}
	proxy, _ := url.Parse("http://proxy.example.test:8080")
	target, _ := url.Parse("https://api.deepseek.com/models")
	if got, err := GuardProxy(http.ProxyURL(proxy))(&http.Request{Method: http.MethodGet, URL: target}); err != nil || got != proxy {
		t.Fatalf("user proxy refused: %v", err)
	}
	upstreamProxy, _ := url.Parse("http://id.reasonix.io:8080")
	if _, err := GuardProxy(http.ProxyURL(upstreamProxy))(&http.Request{Method: http.MethodGet, URL: target}); !errors.Is(err, ErrBlocked) {
		t.Fatal("upstream proxy admitted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://id.reasonix.io/me", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	live := GuardClient(&http.Client{})
	response, err := live.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if _, err := live.Get(server.URL + "/redirect"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("redirect reached upstream: %v", err)
	}
	for _, addr := range []string{"github.com:443", "10.0.0.1:8080", "api.deepseek.com:8443"} {
		if err := CheckAddress(addr); err != nil {
			t.Fatalf("user stream refused: %s: %v", addr, err)
		}
	}
	if err := CheckAddress("remote.reasonix.io:443"); !errors.Is(err, ErrBlocked) {
		t.Fatal("upstream stream admitted")
	}
}

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
