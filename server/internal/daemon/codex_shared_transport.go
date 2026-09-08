package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func sharedDesktopCall(executable, method string, params any) (json.RawMessage, error) {
	s := newCodexCatalogSession(executable)
	address := os.Getenv("SESSION_WALL_UI_URL")
	if address == "" {
		address = "ws://127.0.0.1:19515"
	}
	path := os.Getenv("SESSION_WALL_UI_TOKEN_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, "Library", "Application Support", "SessionWall", "ui-control-token")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("客户端控制服务尚未启动；消息未发送，输入已保留")
	}
	token, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(token))) != 64 {
		return nil, errors.New("客户端控制凭据不可用；消息未发送")
	}
	if err := s.startSharedWithHeaders(address, http.Header{"Authorization": []string{"Bearer " + strings.TrimSpace(string(token))}}); err != nil {
		return nil, err
	}
	defer s.resetLocked()
	return s.client.call(method, params)
}

// A separate connection can subscribe to the same app-server thread without
// launching another process that competes for the thread's writer lease.
type codexWebSocketWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *codexWebSocketWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *codexCatalogSession) startSharedLocked(address string) error {
	return s.startSharedWithHeaders(address, nil)
}

func (s *codexCatalogSession) startSharedWithHeaders(address string, headers http.Header) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "ws" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.User != nil {
		return errors.New("shared Codex endpoint must be a local ws:// address")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(address, headers)
	if err != nil {
		return errors.New("网页控制服务尚未就绪，本条消息未发送，输入已保留；无需重启 Codex")
	}
	conn.SetReadLimit(32 * 1024 * 1024)
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			if _, err := writer.Write(append(data, '\n')); err != nil {
				return
			}
		}
	}()
	closeConnection := func() { _ = conn.Close(); _ = reader.Close() }
	client := newCodexCatalogClient(&codexWebSocketWriter{conn: conn}, reader)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	raw, err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "session-wall", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err == nil {
		err = client.notify("initialized")
	}
	if err != nil {
		closeConnection()
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	var result struct {
		CodexHome string `json:"codexHome"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		closeConnection()
		return err
	}
	s.client = client
	s.cancel = closeConnection
	s.codexHome = strings.TrimSpace(result.CodexHome)
	s.loadedThreads = make(map[string]bool)
	return nil
}
