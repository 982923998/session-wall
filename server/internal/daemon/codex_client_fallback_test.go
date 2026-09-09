package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexClientFallbackSend(t *testing.T) {
	var called string
	handler := codexClientFallbackHandler(func(method string, params any) (json.RawMessage, error) {
		called = method
		input := params.(map[string]any)
		if input["message"] != "draft" || input["model"] != nil {
			t.Fatalf("native settings should be preserved: %+v", input)
		}
		return json.RawMessage(`{"message_id":"receipt","state":"submitted"}`), nil
	})
	request := httptest.NewRequest(http.MethodPost, "/?action=client-send&thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a&model=other", strings.NewReader("draft"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || called != "sessionWall/send" || !strings.Contains(response.Body.String(), `"state":"submitted"`) {
		t.Fatalf("native submission: %s %d %s", called, response.Code, response.Body.String())
	}
}

func TestCodexClientFallbackGuards(t *testing.T) {
	handler := codexClientFallbackHandler(func(string, any) (json.RawMessage, error) {
		t.Fatal("invalid request reached native UI")
		return nil, nil
	})
	for _, tc := range []struct {
		method, query, origin string
		status                int
	}{
		{http.MethodGet, "action=client-send&thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "action=client-send&thread_id=invalid", "", http.StatusBadRequest},
		{http.MethodPost, "action=client-send&thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a", "https://example.com", http.StatusForbidden},
		{http.MethodGet, "action=client-status&thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a", "https://example.com", http.StatusForbidden},
	} {
		r := httptest.NewRequest(tc.method, "/?"+tc.query, strings.NewReader("draft"))
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.query, w.Code, tc.status)
		}
	}
}

func TestCodexClientFallbackPublicStatusDoesNotStartExecutor(t *testing.T) {
	// A missing helper fails closed instead of spawning any Codex executable.
	t.Setenv("SESSION_WALL_UI_TOKEN_FILE", t.TempDir()+"/missing")
	handler := NewCodexSendMessageHandler("/nonexistent/codex")
	r := httptest.NewRequest(http.MethodGet, "/?action=client-status&thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a&message_id=test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "客户端控制服务") {
		t.Fatalf("status unexpectedly used normal executor: %d %s", w.Code, w.Body.String())
	}
}
