package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeCodexThreadListResult(t *testing.T) {
	raw := json.RawMessage(`{
		"data": [{
			"id": "thread-1",
			"name": "修改 Discussion",
			"preview": "A long preview",
			"cwd": "/tmp/manuscript",
			"createdAt": 10,
			"updatedAt": 20,
			"recencyAt": 30,
			"isPinned": true,
			"status": {"type": "idle"},
			"gitInfo": {"branch": "main"}
		}],
		"nextCursor": null
	}`)

	threads, err := decodeCodexThreadListResult(raw)
	if err != nil {
		t.Fatalf("decodeCodexThreadListResult() error = %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("thread count = %d, want 1", len(threads))
	}
	got := threads[0]
	if got.ID != "thread-1" || got.Name != "修改 Discussion" || got.Status != "idle" {
		t.Fatalf("thread = %+v", got)
	}
	if got.Branch != "main" || got.RecencyAt != 30 || !got.Pinned {
		t.Fatalf("thread metadata = %+v", got)
	}
}

func TestCodexOpenThreadHandlerUsesFixedDeepLink(t *testing.T) {
	var opened string
	handler := codexOpenThreadHandler(func(_ context.Context, target string) error {
		opened = target
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/codex/open-thread", strings.NewReader("019ed804-66d5-7361-b92a-996db5525f27"))
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if opened != "codex://threads/019ed804-66d5-7361-b92a-996db5525f27" {
		t.Fatalf("opened = %q", opened)
	}
}

func TestCodexOpenThreadHandlerRejectsInvalidID(t *testing.T) {
	handler := codexOpenThreadHandler(func(context.Context, string) error {
		t.Fatal("opener must not run")
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/codex/open-thread", strings.NewReader("https://example.com"))
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCodexThreadsHandlerReturnsLocalCatalog(t *testing.T) {
	handler := codexThreadsHandler(func(context.Context) ([]CodexThreadSummary, error) {
		return []CodexThreadSummary{{ID: "thread-1", Name: "Existing conversation"}}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/codex/threads", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	var response struct {
		Threads []CodexThreadSummary `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Threads) != 1 || response.Threads[0].ID != "thread-1" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCodexThreadsHandlerRejectsWritesAndForeignOrigins(t *testing.T) {
	handler := codexThreadsHandler(func(context.Context) ([]CodexThreadSummary, error) {
		t.Fatal("lister must not run")
		return nil, nil
	})

	for _, tc := range []struct {
		name   string
		method string
		origin string
		want   int
	}{
		{name: "write", method: http.MethodPost, origin: "http://localhost:3000", want: http.StatusMethodNotAllowed},
		{name: "foreign", method: http.MethodGet, origin: "https://example.com", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/codex/threads", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
