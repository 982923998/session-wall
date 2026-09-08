package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeMessageHandlerNeverResumesAnIndependentWriter(t *testing.T) {
	h := nativeCodexMessageHandler(func(method string, params any) (json.RawMessage, error) {
		if method != "sessionWall/send" {
			t.Fatalf("unexpected execution route: %s", method)
		}
		if params.(map[string]any)["message"] != "test message" {
			t.Fatal(params)
		}
		return json.RawMessage(`{"message_id":"local-id","state":"submitted"}`), nil
	})
	r := httptest.NewRequest("POST", "/codex/send-message?thread_id="+wallTaskTestID, strings.NewReader("test message"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 || !strings.Contains(w.Body.String(), "submitted") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestNativeMessageStatusAndStopRequireActualConfirmation(t *testing.T) {
	h := nativeCodexMessageHandler(func(method string, _ any) (json.RawMessage, error) {
		if method == "sessionWall/messageStatus" {
			return json.RawMessage(`{"message_id":"local-id","state":"unknown"}`), nil
		}
		if method != "sessionWall/stop" {
			t.Fatal(method)
		}
		return json.RawMessage(`{"stopped":false}`), nil
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/codex/send-message?action=status&thread_id="+wallTaskTestID+"&message_id=local-id", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "unknown") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/codex/send-message?action=stop&thread_id="+wallTaskTestID, nil))
	if w.Code != 409 {
		t.Fatal("unconfirmed stop was reported as successful")
	}
}
