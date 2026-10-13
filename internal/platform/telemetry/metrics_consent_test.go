package telemetry

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
)

func queueMetrics(t *testing.T, home string, from surface.Surface) {
	t.Helper()
	if err := appendPending(home, pendingPayload{Version: "v1.20.0", OS: "linux", Surface: from.String(), Counters: []Counter{{Signal: "turns", Bucket: "count", Count: 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsConsentWithdrawalStopsRecordingAndSending(t *testing.T) {
	clearPolicyEnv(t)
	home := testenv.TempDir(t)
	allowed, requests := true, 0
	client := testClient(home, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return telemetryResponse(http.StatusAccepted), nil
	}))
	client.surface = surface.Studio
	client.metricsConsent = func() bool { return allowed }
	r := &Reporter{home: home, version: "v1.20.0", surface: surface.Studio, client: client}
	sink := r.Wrap(&readinessSink{})
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.TurnDone})
	allowed = false
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if err := client.flushPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("withdrawn consent uploaded %d metrics requests", requests)
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("withdrawn consent kept pending counters: %v error=%v", entries, err)
	}
	allowed = true
	if err := client.flushPending(t.Context()); err != nil || requests != 0 {
		t.Fatalf("re-enabled metrics sent revoked counters: requests=%d error=%v", requests, err)
	}
}

func TestMetricsConsentIsCheckedAtUploadBoundary(t *testing.T) {
	clearPolicyEnv(t)
	home := testenv.TempDir(t)
	queueMetrics(t, home, surface.Studio)
	requests, checks := 0, 0
	client := testClient(home, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return telemetryResponse(http.StatusAccepted), nil
	}))
	client.surface = surface.Studio
	client.metricsConsent = func() bool {
		checks++
		return checks == 1
	}
	if err := client.flushPending(context.Background()); !errors.Is(err, errMetricsDisabled) || requests != 0 {
		t.Fatalf("upload after revocation: requests=%d error=%v", requests, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("revocation kept claimed uploads: entries=%v error=%v", entries, err)
	}
}

func TestMetricsFlushAndCleanupRespectSurfaceConsent(t *testing.T) {
	clearPolicyEnv(t)
	home := testenv.TempDir(t)
	queueMetrics(t, home, surface.CLI)
	queueMetrics(t, home, surface.Studio)
	posted := flushCollecting(t, home)
	if len(posted) != 1 || posted[0].Surface != surface.CLI.String() {
		t.Fatalf("CLI sent counters outside its consent: %+v", posted)
	}
	queueMetrics(t, home, surface.CLI)
	claimed, err := claimPendingFiles(filepath.Join(home, pendingDirName), time.Now())
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim queue: %v error=%v", claimed, err)
	}
	if err := CleanupSurface(home, surface.Studio); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("surface cleanup removed another surface or kept Studio: entries=%v error=%v", entries, err)
	}
}

func TestMetricsOptOutWhileRunningDiscardsPending(t *testing.T) {
	clearPolicyEnv(t)
	home := testenv.TempDir(t)
	queueMetrics(t, home, surface.Studio)
	client := testClient(home, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("environment opt-out still sent metrics")
		return nil, nil
	}))
	client.surface = surface.Studio
	t.Setenv("DO_NOT_TRACK", "1")
	if err := client.flushPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(home, pendingDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("environment opt-out retained counters: %v error=%v", entries, err)
	}
}
