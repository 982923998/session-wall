package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestCodexSharedTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request struct {
				ID     *int   `json:"id"`
				Method string `json:"method"`
			}
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			if request.ID == nil {
				continue
			}
			var result any = map[string]any{"thread": map[string]any{"id": "shared-thread"}}
			if request.Method == "initialize" {
				result = map[string]any{"codexHome": "/tmp/shared-codex"}
			}
			if err := conn.WriteJSON(map[string]any{"id": *request.ID, "result": result}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	t.Setenv("SESSION_WALL_CODEX_WS_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	s := newCodexCatalogSession("/missing/no-cli-fallback")
	if err := s.startLocked(); err != nil {
		t.Fatal(err)
	}
	defer s.resetLocked()
	raw, err := s.client.call("thread/read", map[string]any{"threadId": "shared-thread"})
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Thread struct{ ID string } }
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Thread.ID != "shared-thread" || s.cmd != nil || s.codexHome != "/tmp/shared-codex" {
		t.Fatalf("unexpected shared connection: %s", raw)
	}
}

func TestCodexSharedTransportRejectsRemoteEndpoint(t *testing.T) {
	s := newCodexCatalogSession("/missing/no-cli-fallback")
	if err := s.startSharedLocked("ws://example.com:19515"); err == nil {
		t.Fatal("remote endpoint accepted")
	}
}
