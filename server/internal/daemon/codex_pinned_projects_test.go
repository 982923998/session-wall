package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexPinnedProjectsRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	config := filepath.Join(home, "selection.json")
	if err := os.WriteFile(config, []byte(`{"source":"codex-pinned"}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex-global-state.json")
	for _, tc := range []struct {
		pinned string
		want   string
	}{
		{`["a"]`, "a"}, {`["b"]`, "b"}, {`[]`, ""},
	} {
		data := `{"pinned-project-ids":` + tc.pinned + `,"local-projects":{"a":{"name":"A","rootPaths":["/a"]},"b":{"name":"B","rootPaths":["/b"]}}}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		projects, err := readCodexProjectSelection(config)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == "" {
			if len(projects) != 0 {
				t.Fatal(projects)
			}
			continue
		}
		if len(projects) != 1 || projects[0].ID != tc.want {
			t.Fatal(projects)
		}
	}
	if err := os.WriteFile(path, []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCodexProjectSelection(config); err == nil {
		t.Fatal("must not silently fall back to all projects")
	}
}
