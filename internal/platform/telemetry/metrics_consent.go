package telemetry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/surface"
)

var errMetricsDisabled = errors.New("telemetry: metrics consent withdrawn")

func (c *Client) metricsAllowed() bool {
	if OptedOut() || (c.metricsConsent != nil && !c.metricsConsent()) {
		_ = CleanupSurface(c.home, c.reportedSurface())
		return false
	}
	return true
}

// CleanupSurface discards counters for a surface without revoking another
// surface's independent consent. It also removes claimed uploads.
func CleanupSurface(home string, from surface.Surface) error {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	dir := filepath.Join(home, pendingDirName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".json.uploading")) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p pendingPayload
		if json.Unmarshal(b, &p) == nil && queuedSurface(p) == from.Or(surface.CLI) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}
