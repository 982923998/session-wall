package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type CodexThreadSettings struct {
	ThreadID        string  `json:"thread_id"`
	Model           string  `json:"model"`
	ReasoningEffort string  `json:"reasoning_effort"`
	ServiceTier     *string `json:"service_tier"`
	FastMode        *bool   `json:"fast_mode"`
}

func configuredServiceTier(data []byte) *string {
	type settings struct {
		ServiceTier *string `toml:"service_tier"`
	}
	var config struct {
		ServiceTier *string             `toml:"service_tier"`
		Profile     string              `toml:"profile"`
		Profiles    map[string]settings `toml:"profiles"`
	}
	if toml.Unmarshal(data, &config) != nil {
		return nil
	}
	if profile, ok := config.Profiles[config.Profile]; ok && profile.ServiceTier != nil {
		return profile.ServiceTier
	}
	return config.ServiceTier
}

func fastModeForTier(tier *string) *bool {
	if tier == nil {
		return nil
	}
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(*tier)) {
	case "priority", "fast", "ultrafast":
		enabled = true
	case "default", "standard", "flex":
		enabled = false
	default:
		return nil
	}
	return &enabled
}

func readCodexThreadSettings(ctx context.Context, home, id string) (CodexThreadSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	query := "SELECT model, reasoning_effort FROM threads WHERE id=" + sqliteStringLiteral(id) + ";"
	data, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", filepath.Join(home, "state_5.sqlite"), query).Output()
	if err != nil {
		return CodexThreadSettings{}, err
	}
	var rows []CodexThreadSettings
	if err := json.Unmarshal(data, &rows); err != nil {
		return CodexThreadSettings{}, err
	}
	if len(rows) != 1 {
		return CodexThreadSettings{}, fmt.Errorf("task settings unavailable")
	}
	result := rows[0]
	result.ThreadID = id
	if config, err := os.ReadFile(filepath.Join(home, "config.toml")); err == nil {
		result.ServiceTier = configuredServiceTier(config)
		result.FastMode = fastModeForTier(result.ServiceTier)
	}
	return result, nil
}

// Read persisted task settings and the same service-tier configuration used by the desktop.
func NewCodexThreadSettingsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !localCatalogOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("thread_id")
		if !codexThreadIDPattern.MatchString(id) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		settings, err := readCodexThreadSettings(r.Context(), defaultCodexHome(), id)
		if err != nil {
			http.Error(w, "无法读取任务设置", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(settings)
	})
}
