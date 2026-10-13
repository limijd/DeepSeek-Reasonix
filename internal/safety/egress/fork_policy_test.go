package egress

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"reasonix/internal/base/netpolicy"
)

func TestCleanForkPreservesAllowlistAndBlocksUpstream(t *testing.T) {
	if os.Getenv("REASONIX_TEST_FIXED_EGRESS") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCleanForkPreservesAllowlistAndBlocksUpstream$")
		cmd.Env = append(os.Environ(), "REASONIX_TEST_FIXED_EGRESS=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixed policy: %v\n%s", err, output)
		}
		return
	}
	netpolicy.Enable()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host) }))
	defer backend.Close()
	h := newHarness(t, Policy{Allow: []string{"github.com", "reasonix.io", "*.reasonix.io"}}, Options{}, map[string]string{"github.com": "93.184.216.34"}, backend.Listener.Addr().String())
	h.proxy.Ask("fixed", func(string) (bool, error) { t.Error("upstream reached approval"); return true, nil })
	response, body := get(t, h.client("fixed"), "http://github.com/")
	if response.StatusCode != http.StatusOK || body != "github.com" {
		t.Fatalf("user allowlist lost: %d %q", response.StatusCode, body)
	}
	for _, target := range []string{"http://reasonix.io/", "http://id.reasonix.io/", "http://remote.reasonix.io/"} {
		response, _ := get(t, h.client("fixed"), target)
		if response.StatusCode != http.StatusForbidden || response.Header.Get("X-Proxy-Error") != string(ReasonDenied) {
			t.Fatalf("upstream admitted: %s: %d", target, response.StatusCode)
		}
	}
	if len(h.dialed()) != 1 {
		t.Fatalf("upstream caused a dial: %v", h.dialed())
	}
}
