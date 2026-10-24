package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadCodexThreadSettings(t *testing.T) {
	home := t.TempDir()
	if out, err := exec.Command("sqlite3", filepath.Join(home, "state_5.sqlite"), "CREATE TABLE threads(id TEXT,model TEXT,reasoning_effort TEXT); INSERT INTO threads VALUES('task','actual-model','high');").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, tc := range []struct {
		config    string
		known, on bool
	}{
		{"service_tier = 'priority'\n", true, true},
		{"service_tier = 'priority'\nprofile = 'normal'\n[profiles.normal]\nservice_tier = 'default'\n", true, false},
		{"model = 'other-global-model'\n", false, false},
		{"service_tier = [", false, false},
	} {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := readCodexThreadSettings(context.Background(), home, "task")
		if err != nil {
			t.Fatal(err)
		}
		if result.Model != "actual-model" || result.ReasoningEffort != "high" {
			t.Fatal(result)
		}
		if (result.FastMode != nil) != tc.known || (tc.known && *result.FastMode != tc.on) {
			t.Fatalf("config=%q settings=%+v", tc.config, result)
		}
	}
}
