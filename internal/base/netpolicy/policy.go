// Package netpolicy enforces the fork's fixed outbound network boundary.
package netpolicy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

var ErrBlocked = errors.New("upstream Reasonix services are disabled in this fork")

var enabled atomic.Bool
var install sync.Once

// Enable fixes the process policy for its lifetime; configuration cannot relax it.
func Enable() {
	install.Do(func() {
		enabled.Store(true)
		base := BaseTransport(http.DefaultTransport)
		if tr, ok := base.(*http.Transport); ok {
			clone := tr.Clone()
			clone.Proxy = GuardProxy(clone.Proxy)
			base = clone
		}
		http.DefaultTransport = GuardTransport(base)
	})
}

func Active() bool { return enabled.Load() }

// AllowedHost excludes upstream services without changing user endpoints or LAN access.
func AllowedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host != "reasonix.io" && !strings.HasSuffix(host, ".reasonix.io") && host != "api.typesafe.ai"
}

func CheckURL(u *url.URL) error {
	if !Active() {
		return nil
	}
	if u == nil || !AllowedHost(u.Hostname()) {
		return ErrBlocked
	}

	switch u.Scheme {
	case "http", "https", "ws", "wss":
		return nil
	default:
		return ErrBlocked
	}
}

func CheckAddress(addr string) error {
	if !Active() {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if !AllowedHost(host) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}

func GuardProxy(proxy func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if err := CheckURL(req.URL); err != nil {
			return nil, err
		}
		if proxy == nil {
			return nil, nil
		}
		u, err := proxy(req)
		if err != nil || u == nil {
			return u, err
		}
		if Active() && !AllowedHost(u.Hostname()) {
			return nil, ErrBlocked
		}
		return u, nil
	}
}

type guardedTransport struct{ base http.RoundTripper }

func (t guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := CheckURL(req.URL); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func (t guardedTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func BaseTransport(t http.RoundTripper) http.RoundTripper {
	if guarded, ok := t.(guardedTransport); ok {
		return guarded.base
	}
	return t
}

func GuardTransport(t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	if !Active() {
		return t
	}
	return guardedTransport{base: BaseTransport(t)}
}

func GuardClient(c *http.Client) *http.Client {
	if c == nil {
		c = &http.Client{}
	}
	copy := *c
	copy.Transport = GuardTransport(copy.Transport)
	return &copy
}
