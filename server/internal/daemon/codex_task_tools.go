package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type wallToolThread struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CWD       string `json:"cwd"`
	ProjectID string `json:"projectId"`
	Status    struct {
		Type string `json:"type"`
	} `json:"status"`
}

func readWallToolThread(call codexCall, id string) (wallToolThread, error) {
	raw, err := call("thread/read", map[string]any{"threadId": id, "includeTurns": false})
	var result struct {
		Thread wallToolThread `json:"thread"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	if err == nil && (result.Thread.ID != id || result.Thread.CWD == "") {
		err = errors.New("invalid thread identity")
	}
	return result.Thread, err
}

func isWallRouter(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "router" || strings.HasPrefix(name, "router · ") || strings.HasSuffix(name, " router")
}

func wallRouterToolConfig(call codexCall, executable, hostExecutable, id string) map[string]any {
	thread, err := readWallToolThread(call, id)
	if err != nil || !isWallRouter(thread.Name) {
		return nil
	}
	return map[string]any{"mcp_servers.session_wall": map[string]any{
		"command":  hostExecutable,
		"args":     []string{"session-wall", "task-tools", "--codex-path", executable, "--parent-thread-id", id},
		"required": false,
	}}
}

// createWallTask creates a durable, named, empty local task. It deliberately
// does not start a turn, change permissions, or claim delivery of a handoff.
func createWallTask(call codexCall, parent wallToolThread, title string) (wallToolThread, error) {
	parts := strings.SplitN(title, " · ", 2)
	if title != strings.TrimSpace(title) || len(title) > 240 || len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return wallToolThread{}, errors.New("title must be nonempty role · function")
	}
	// Paginated empty threads are not restorable in the bundled runtime.
	// Legacy history persists the empty task without an artificial model turn.
	params := map[string]any{"cwd": parent.CWD, "ephemeral": false, "historyMode": "legacy"}
	if parent.ProjectID != "" {
		params["projectId"] = parent.ProjectID
	}
	raw, err := call("thread/start", params)
	if err != nil {
		return wallToolThread{}, err
	}
	var result struct {
		Thread wallToolThread `json:"thread"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return wallToolThread{}, err
	}
	id := result.Thread.ID
	if !codexThreadIDPattern.MatchString(id) {
		return wallToolThread{}, errors.New("created thread has no valid final ID")
	}
	if _, err = call("thread/name/set", map[string]any{"threadId": id, "name": title}); err != nil {
		return wallToolThread{}, fmt.Errorf("task %s exists but naming failed; do not retry creation: %w", id, err)
	}
	thread, err := readWallToolThread(call, id)
	if err != nil || thread.Name != title {
		return wallToolThread{}, fmt.Errorf("task %s exists but name readback failed; do not retry creation", id)
	}
	return thread, nil
}

// ServeCodexTaskTools is a stdio MCP server scoped to the calling Router.
// It exposes no HTTP port and never uses the desktop app's private bridge.
func ServeCodexTaskTools(executable, parentID string, input io.Reader, output io.Writer) error {
	if !codexThreadIDPattern.MatchString(parentID) {
		return errors.New("invalid parent task ID")
	}
	session := newCodexCatalogSession(executable)
	defer session.resetLocked()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ProtocolVersion string `json:"protocolVersion"`
				Name            string `json:"name"`
				Arguments       struct {
					Title    string `json:"title"`
					ThreadID string `json:"threadId"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		if len(request.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": request.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "session-wall-task-tools", "version": "1"}}
		case "ping":
			response["result"] = map[string]any{}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{
				map[string]any{"name": "create_thread", "description": "Only on explicit user request, create a named EMPTY Codex task in this Router's local project directory (no worktree). Required title: role · function. Returns verified final ID, name and status. Does NOT send a prompt or run the new task. Do not claim a handoff was delivered. Do not retry after an error that includes a created ID.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}, "required": []string{"title"}, "additionalProperties": false}},
				map[string]any{"name": "read_thread", "description": "Read back a task in this Router's project to verify its final name and status.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"threadId": map[string]any{"type": "string"}}, "required": []string{"threadId"}, "additionalProperties": false}},
			}}
		case "tools/call":
			thread, err := func() (wallToolThread, error) {
				if request.Params.Name != "create_thread" && request.Params.Name != "read_thread" {
					return wallToolThread{}, errors.New("unknown tool")
				}
				if err := session.startLocked(); err != nil {
					return wallToolThread{}, err
				}
				parent, err := readWallToolThread(session.client.call, parentID)
				if err != nil {
					return wallToolThread{}, err
				}
				if !isWallRouter(parent.Name) {
					return wallToolThread{}, errors.New("caller is not a named Router")
				}
				if info, err := os.Stat(parent.CWD); err != nil || !info.IsDir() {
					return wallToolThread{}, errors.New("Router directory is unavailable")
				}
				if request.Params.Name == "create_thread" {
					created, err := createWallTask(session.client.call, parent, request.Params.Arguments.Title)
					if err != nil {
						return wallToolThread{}, err
					}
					// Verify durability through a separate process, not the creator's cache.
					verifier := newCodexCatalogSession(executable)
					defer verifier.resetLocked()
					if err := verifier.startLocked(); err != nil {
						return wallToolThread{}, fmt.Errorf("task %s exists but independent verification failed; do not retry creation: %w", created.ID, err)
					}
					verified, err := readWallToolThread(verifier.client.call, created.ID)
					if err != nil || verified.Name != created.Name || verified.CWD != parent.CWD {
						return wallToolThread{}, fmt.Errorf("task %s exists but independent readback failed; do not retry creation", created.ID)
					}
					return verified, nil
				}
				thread, err := readWallToolThread(session.client.call, request.Params.Arguments.ThreadID)
				if err == nil && thread.CWD != parent.CWD {
					err = errors.New("task is outside this Router directory")
				}
				return thread, err
			}()
			body, _ := json.Marshal(thread)
			if err != nil {
				body = []byte(err.Error())
			}
			response["result"] = map[string]any{"isError": err != nil, "content": []any{map[string]any{"type": "text", "text": string(body)}}}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
