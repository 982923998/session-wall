package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	codexThreadListTimeout = 20 * time.Second
	codexPreviewLimit      = 320
)

// CodexThreadSummary is the read-only catalog entry consumed by the local card wall.
type CodexThreadSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Preview   string `json:"preview,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Status    string `json:"status,omitempty"`
	Branch    string `json:"branch,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	RecencyAt int64  `json:"recency_at,omitempty"`
	Pinned    bool   `json:"pinned"`
	AgentRole string `json:"agent_role,omitempty"`
	AgentName string `json:"agent_nickname,omitempty"`
}

type codexThreadListResult struct {
	Data []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Preview   string `json:"preview"`
		CWD       string `json:"cwd"`
		CreatedAt int64  `json:"createdAt"`
		UpdatedAt int64  `json:"updatedAt"`
		RecencyAt int64  `json:"recencyAt"`
		IsPinned  bool   `json:"isPinned"`
		AgentRole string `json:"agentRole"`
		AgentName string `json:"agentNickname"`
		Status    struct {
			Type string `json:"type"`
		} `json:"status"`
		GitInfo struct {
			Branch string `json:"branch"`
		} `json:"gitInfo"`
	} `json:"data"`
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func decodeCodexThreadListResult(raw json.RawMessage) ([]CodexThreadSummary, error) {
	var result codexThreadListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode Codex thread list: %w", err)
	}
	threads := make([]CodexThreadSummary, 0, len(result.Data))
	for _, thread := range result.Data {
		if strings.TrimSpace(thread.ID) == "" {
			continue
		}
		threads = append(threads, CodexThreadSummary{
			ID:        thread.ID,
			Name:      truncateRunes(thread.Name, 160),
			Preview:   truncateRunes(thread.Preview, codexPreviewLimit),
			CWD:       thread.CWD,
			Status:    thread.Status.Type,
			Branch:    thread.GitInfo.Branch,
			CreatedAt: thread.CreatedAt,
			UpdatedAt: thread.UpdatedAt,
			RecencyAt: thread.RecencyAt,
			Pinned:    thread.IsPinned,
			AgentRole: thread.AgentRole,
			AgentName: thread.AgentName,
		})
	}
	return threads, nil
}

type codexRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type codexCatalogClient struct {
	stdin  io.Writer
	stdout *bufio.Scanner
	nextID int
}

func newCodexCatalogClient(stdin io.Writer, stdout io.Reader) *codexCatalogClient {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	return &codexCatalogClient{stdin: stdin, stdout: scanner}
}

func (c *codexCatalogClient) call(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := json.NewEncoder(c.stdin).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("send Codex %s: %w", method, err)
	}
	for c.stdout.Scan() {
		var response codexRPCResponse
		if err := json.Unmarshal(c.stdout.Bytes(), &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("Codex %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
		}
		return response.Result, nil
	}
	if err := c.stdout.Err(); err != nil {
		return nil, fmt.Errorf("read Codex %s: %w", method, err)
	}
	return nil, fmt.Errorf("read Codex %s: app-server closed", method)
}

func (c *codexCatalogClient) notify(method string) error {
	return json.NewEncoder(c.stdin).Encode(map[string]any{"method": method, "params": map[string]any{}})
}

func listCodexThreads(ctx context.Context, executable string) ([]CodexThreadSummary, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("Codex executable is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, codexThreadListTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	client := newCodexCatalogClient(stdin, stdout)
	if _, err := client.call("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "multica-session-card-wall",
			"title":   "Multica Session Card Wall",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": false},
	}); err != nil {
		return nil, err
	}
	if err := client.notify("initialized"); err != nil {
		return nil, fmt.Errorf("notify Codex initialized: %w", err)
	}
	raw, err := client.call("thread/list", map[string]any{
		"cursor":         nil,
		"limit":          500,
		"sortKey":        "recency_at",
		"sortDirection":  "desc",
		"archived":       false,
		"modelProviders": []string{},
		"sourceKinds":    []string{"cli", "vscode", "appServer", "exec"},
	})
	if err != nil {
		return nil, err
	}
	return decodeCodexThreadListResult(raw)
}

func localCatalogOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func codexThreadsHandler(list func(context.Context) ([]CodexThreadSummary, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		origin := r.Header.Get("Origin")
		if !localCatalogOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		threads, err := list(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"threads": threads})
	})
}

// NewCodexThreadCatalogHandler exposes the read-only catalog without starting
// the task daemon. The focused Session Wall build uses this local-only surface.
func NewCodexThreadCatalogHandler(executable string) http.Handler {
	return codexThreadsHandler(func(ctx context.Context) ([]CodexThreadSummary, error) {
		return listCodexThreads(ctx, executable)
	})
}

func (d *Daemon) codexThreadsHandler() http.Handler {
	return codexThreadsHandler(func(ctx context.Context) ([]CodexThreadSummary, error) {
		entry, ok := d.agents()["codex"]
		if !ok {
			return nil, errors.New("Codex runtime is not available on this daemon")
		}
		entry, _ = d.resolveAgentEntry(ctx, "codex", entry)
		return listCodexThreads(ctx, entry.Path)
	})
}
