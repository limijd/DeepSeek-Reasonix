package config

import "testing"

func TestProjectCannotOverrideDesktopPrivacy(t *testing.T) {
	for _, tt := range []struct {
		name, user, project string
		telemetry, metrics  bool
	}{
		{"disabled", "[desktop]\ntelemetry=false\nmetrics=false\n", "[desktop]\ntelemetry=true\nmetrics=true\n", false, false},
		{"enabled", "[desktop]\ntelemetry=true\nmetrics=true\n", "[desktop]\ntelemetry=false\nmetrics=false\n", true, true},
		{"independent", "[desktop]\ntelemetry=true\nmetrics=false\n", "[desktop]\ntelemetry=false\nmetrics=true\n", true, false},
		{"defaults", "", "[desktop]\ntelemetry=false\nmetrics=false\n", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := loadScoped(t, tt.user, tt.project)
			if cfg.DesktopTelemetry() != tt.telemetry || cfg.DesktopMetrics() != tt.metrics {
				t.Fatalf("telemetry=%v metrics=%v, want %v %v", cfg.DesktopTelemetry(), cfg.DesktopMetrics(), tt.telemetry, tt.metrics)
			}
		})
	}
}
