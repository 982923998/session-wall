package daemon

import (
	"context"
	"encoding/json"
	"net/http"
)

// The native route is opt-in; regular sending never invokes UI automation.
func codexClientFallbackHandler(call codexCall) http.Handler {
	send := codexSendMessageHandler(func(ctx context.Context, id, message string, options CodexMessageOptions) (CodexMessageResult, error) {
		raw, err := call("sessionWall/send", map[string]any{"threadId": id, "message": message})
		if err != nil {
			return CodexMessageResult{}, err
		}
		var result CodexMessageResult
		err = json.Unmarshal(raw, &result)
		return result, err
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") == "client-send" {
			send.ServeHTTP(w, r)
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
		id := r.URL.Query().Get("thread_id")
		if !codexThreadIDPattern.MatchString(id) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet || r.URL.Query().Get("action") != "client-status" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, err := call("sessionWall/messageStatus", map[string]any{"threadId": id, "messageId": r.URL.Query().Get("message_id")})
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
}
