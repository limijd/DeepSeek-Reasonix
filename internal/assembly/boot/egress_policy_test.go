package boot

import (
	"bytes"
	"errors"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
)

func TestDenyOnlyEgressPolicyIsRejected(t *testing.T) {
	for _, mode := range []string{"enforce", "off"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Default()
			cfg.Sandbox.AllowedDomains = nil
			cfg.Sandbox.DeniedDomains = []string{"blocked.example"}
			env := toolEnvironment{network: true, bash: sandbox.Spec{Mode: mode, Network: true}}
			var warning bytes.Buffer
			if err := env.routeEgress(cfg, &warning); !errors.Is(err, errDenyOnlyEgress) {
				t.Fatalf("error=%v, want unsupported egress policy refusal", err)
			}
		})
	}
}
