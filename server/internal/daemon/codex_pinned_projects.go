package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Read each refresh from the desktop's persisted sidebar, never a cached list.
func readCodexPinnedProjects(path string) ([]CodexProjectSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state struct {
		Pinned   []string `json:"pinned-project-ids"`
		Projects map[string]struct {
			Name  string   `json:"name"`
			Roots []string `json:"rootPaths"`
		} `json:"local-projects"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Pinned == nil {
		return nil, fmt.Errorf("Codex pinned project list is unavailable")
	}
	projects := make([]CodexProjectSummary, 0, len(state.Pinned))
	seen := make(map[string]bool)
	for _, id := range state.Pinned {
		if seen[id] {
			continue
		}
		seen[id] = true
		project, ok := state.Projects[id]
		if !ok {
			return nil, fmt.Errorf("Codex pinned project %s has no local project metadata", id)
		}
		if strings.TrimSpace(project.Name) == "" || len(project.Roots) == 0 {
			return nil, fmt.Errorf("Codex pinned project %s has incomplete metadata", id)
		}
		projects = append(projects, CodexProjectSummary{ID: id, Name: project.Name, Roots: project.Roots, Position: int64(len(projects))})
	}
	return projects, nil
}
