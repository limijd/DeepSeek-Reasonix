package securitycheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/netpolicy"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/model/billing"
	"reasonix/internal/model/catalog"
	"reasonix/internal/platform/account"
	"reasonix/internal/platform/crashreport"
	"reasonix/internal/platform/feedback"
	"reasonix/internal/platform/remotecloud"
	"reasonix/internal/platform/telemetry"
	"reasonix/internal/platform/update"
)

func TestCleanForkPreservesAgentAndDisablesBackgroundServices(t *testing.T) {
	netpolicy.Enable()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	calls := 0
	client := &http.Client{Transport: cleanTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	if reporter := telemetry.Start(telemetry.Options{Mode: "on", Version: "1.0.0", Interactive: true, HomeDir: t.TempDir()}); reporter != nil {
		t.Fatal("background telemetry started")
	}
	if _, err := account.New("https://background.example.test", "test", client).Me(ctx, "not-a-real-key"); !errors.Is(err, netpolicy.ErrBlocked) {
		t.Fatalf("account service: %v", err)
	}
	if balance, err := billing.FetchWithClient(ctx, client, "https://api.deepseek.com/user/balance", "not-a-real-key"); err != nil || balance != nil {
		t.Fatalf("balance polling: %v %v", balance, err)
	}
	if _, err := update.FetchIndex(ctx, client, "https://github.com/upstream/releases/index.json", "test"); !errors.Is(err, netpolicy.ErrBlocked) {
		t.Fatalf("update catalog: %v", err)
	}
	if _, err := (update.Transport{Client: client}).FetchOnce(ctx, client, "https://github.com/upstream/releases/download/asset", 1024); !errors.Is(err, netpolicy.ErrBlocked) {
		t.Fatalf("update fallback: %v", err)
	}
	if reading := billing.NewCache(client, "https://background.example.test/balance", "not-a-real-key").Read(ctx); reading.Configured || reading.Err != nil {
		t.Fatalf("disabled wallet still appears configured: %+v", reading)
	}
	if err := crashreport.Send(ctx, crashreport.Report{}, netclient.ProxySpec{Mode: netclient.ModeOff}); !errors.Is(err, netpolicy.ErrBlocked) {
		t.Fatalf("crash upload: %v", err)
	}
	service, err := feedback.New(feedback.Config{Home: t.TempDir(), Base: "https://background.example.test", HTTP: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Submit(ctx, feedback.Draft{Category: feedback.Bug, Body: "test", DisplayName: "test", Env: feedback.EnvContext{Surface: feedback.SurfaceStudio}}); !errors.Is(err, netpolicy.ErrBlocked) {
		t.Fatalf("feedback service: %v", err)
	}
	remotecloud.New(account.New("https://background.example.test", "test", client), nil, "wss://background.example.test", "test").Run(ctx)
	if ctx.Err() != nil {
		t.Fatal("disabled cloud relay kept a background loop alive")
	}
	if calls != 0 {
		t.Fatalf("background services reached transport: %d", calls)
	}
	cfg := config.Default()
	on := true
	cfg.Desktop.Telemetry, cfg.Desktop.Metrics, cfg.Desktop.CheckUpdates = &on, &on, &on
	cfg.Telemetry.CLIMetrics = "on"
	if cfg.DesktopTelemetry() || cfg.DesktopMetrics() || cfg.DesktopCheckUpdates() || cfg.CLITelemetryMode() != "off" {
		t.Fatal("old consent re-enabled background services")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": "deepseek-v4-flash"}}})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	normal, err := netclient.NewHTTPClient(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ProbeEndpoint(ctx, catalog.ProbeOptions{BaseURL: server.URL, APIKey: "not-a-real-key", Client: normal}); err != nil {
		t.Fatalf("onboarding model discovery: %v", err)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	command := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf executed > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := hook.DefaultSpawner(ctx, hook.SpawnInput{Command: command, Args: []string{marker}, Mode: hook.ExecutionExec, Timeout: time.Second})
	if result.SpawnErr != nil || result.ExitCode != 0 {
		t.Fatalf("local hook refused: %+v", result)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "executed" {
		t.Fatalf("local hook did not run: %q %v", data, err)
	}
}

type cleanTransport func(*http.Request) (*http.Response, error)

func (f cleanTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
