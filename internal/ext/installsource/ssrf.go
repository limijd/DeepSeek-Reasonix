package installsource

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/netpolicy"
)

// ssrfGuardClient wraps base so every fetch refuses to connect to private,
// link-local, CGNAT, or unspecified addresses — the SSRF surface a prompt-
// injected install source would aim at (cloud metadata at 169.254.169.254,
// RFC1918 internal services). Loopback is allowed: the agent can already reach
// localhost via bash, and the install tests serve over 127.0.0.1. The check
// runs at dial time on the resolved IP and then dials that vetted IP, so a
// public host that DNS-rebinds to an internal address is caught too.
//
// This mirrors web_fetch's guard (internal/tools/builtin/webfetch.go); the
// install_source tool fetches the same kind of untrusted URLs and must not be
// the one un-guarded path. Kept in sync by hand — both block the same set.
func ssrfGuardClient(base *http.Client) *http.Client {
	guarded := *base // copy Timeout etc.
	if t, ok := netpolicy.BaseTransport(base.Transport).(*http.Transport); ok && t != nil {
		ct := t.Clone()
		inner := ct.DialContext
		if inner == nil {
			inner = (&net.Dialer{}).DialContext
		}
		ct.DialContext = ssrfDial(inner)
		proxy := ct.Proxy
		ct.Proxy = nil
		ct.DialTLSContext = nil
		guarded.Transport = ssrfTransport{direct: ct, proxy: proxy}
	} else {
		// Non-*http.Transport (or nil Transport): build a fresh guarded transport.
		// The real paths — boot's netclient and the tests' httptest client — are
		// always *http.Transport, so this branch only covers a bare &http.Client{}.
		guarded.Transport = &http.Transport{DialContext: ssrfDial((&net.Dialer{}).DialContext)}
	}
	return netpolicy.GuardClient(&guarded)
}

type ssrfTransport struct {
	direct *http.Transport
	proxy  func(*http.Request) (*url.URL, error)
}

func (t ssrfTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.proxy == nil {
		return t.direct.RoundTrip(req)
	}
	pu, err := t.proxy(req)
	if err != nil {
		return nil, err
	}
	if pu == nil {
		return t.direct.RoundTrip(req)
	}
	tr := t.direct.Clone()
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	tr.DialContext = ssrfDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return netclient.DialProxy(ctx, pu, dialer, network, addr)
	})
	return tr.RoundTrip(req)
}

var lookupInstallHost = net.DefaultResolver.LookupIPAddr

func ssrfDial(inner func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := lookupInstallHost(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for %s", host)
		}
		for _, ip := range ips {
			if blockedFetchIP(ip.IP) {
				return nil, fmt.Errorf("refusing to fetch internal address %s (resolves to %s)", host, ip.IP)
			}
		}
		// Dial the vetted IP, not the hostname, so the connection can't re-resolve
		// to a different (internal) address (DNS rebinding).
		return inner(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// cgnatRange is RFC 6598 shared address space (100.64.0.0/10). Go's IsPrivate
// doesn't cover it, yet some clouds host instance metadata there (Alibaba Cloud
// at 100.100.100.200), so it's an SSRF target to refuse too.
var cgnatRange = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// blockedFetchIP reports whether ip is an address install_source must not reach.
// Loopback is intentionally allowed (see ssrfGuardClient).
func blockedFetchIP(ip net.IP) bool {
	return ip.IsPrivate() || // RFC1918 + IPv6 unique-local (fc00::/7)
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. cloud metadata) + fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || // 0.0.0.0 / ::
		cgnatRange.Contains(ip) // 100.64.0.0/10 (incl. Alibaba Cloud metadata)
}
