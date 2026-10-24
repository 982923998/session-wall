package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func imageRequest(t *testing.T, data []byte, count int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for i := 0; i < count; i++ {
		part, err := w.CreateFormFile("images", "../../fake.html")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
	}
	_ = w.Close()
	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestCodexImageUploadValidatesContentAndOwnsPaths(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	message, paths, err := readCodexImageMessage(httptest.NewRecorder(), imageRequest(t, pngData.Bytes(), 1), root)
	if err != nil || message != "（图片）" || len(paths) != 1 {
		t.Fatalf("%q %v %v", message, paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pngData.Bytes()) || !strings.HasPrefix(paths[0], root+string(filepath.Separator)) || filepath.Base(paths[0]) != "1.png" {
		t.Fatal(paths)
	}
	if info, err := os.Stat(paths[0]); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("image must be private")
	}
	for _, tc := range []struct {
		data  []byte
		count int
	}{
		{[]byte("<svg onload='alert(1)'></svg>"), 1},
		{make([]byte, codexImageMax+1), 1},
		{pngData.Bytes(), 7},
	} {
		if _, _, err := readCodexImageMessage(httptest.NewRecorder(), imageRequest(t, tc.data, tc.count), t.TempDir()); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
}

func TestCodexQueuedImageArguments(t *testing.T) {
	result, err := queueCodexThreadMessage(func(method string, params any) (json.RawMessage, error) {
		if method != "thread/queue/add" {
			t.Fatalf("must not resume or start: %s", method)
		}
		input := params.(map[string]any)["input"].([]map[string]any)
		if len(input) != 2 || input[0]["text"] != "image" || input[1]["type"] != "localImage" || input[1]["path"] != "/tmp/a b.png" {
			t.Fatal(input)
		}
		return json.RawMessage(`{"queuedSubmission":{"id":"queued-image"}}`), nil
	}, "task", "image", "client-id", "/tmp/a b.png")
	if err != nil || result != "queued-image" {
		t.Fatalf("%s %v", result, err)
	}
}

func TestCodexImageHistoryOnlyExposesOwnedUploads(t *testing.T) {
	path := filepath.Join(codexUploadsDirectory(), "01a08089-fb23-7d42-bfe5-89a968d8a56a", "1.png")
	if codexImageURL(path) == "" || codexImageURL("/etc/passwd") != "" {
		t.Fatal("unsafe image URL mapping")
	}
	w := httptest.NewRecorder()
	NewCodexImageHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/codex/attachments/../../etc/passwd", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
	crossSite := httptest.NewRequest(http.MethodGet, "/codex/attachments/01a08089-fb23-7d42-bfe5-89a968d8a56a/1.png", nil)
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	blocked := httptest.NewRecorder()
	NewCodexImageHandler().ServeHTTP(blocked, crossSite)
	if blocked.Code != http.StatusForbidden {
		t.Fatal(blocked.Code)
	}
	root := t.TempDir()
	id := "01a08089-fb23-7d42-bfe5-89a968d8a56a"
	if err := recordCodexImageMessage(root, id, []string{filepath.Join(root, id, "1.png")}); err != nil {
		t.Fatal(err)
	}
	urls := codexMessageImageURLs(root, id)
	if len(urls) != 1 || !strings.HasSuffix(urls[0], id+"/1.png") {
		t.Fatal(urls)
	}
	if err := recordCodexImageMessage(root, id, []string{"/etc/passwd"}); err == nil {
		t.Fatal("outside upload root accepted")
	}
}

func TestCodexTextJSONCannotSupplyLocalImagePaths(t *testing.T) {
	handler := codexSendMessageHandler(func(_ context.Context, _ string, message string, options CodexMessageOptions) (CodexMessageResult, error) {
		if message != "plain text" || len(options.ImagePaths) != 0 {
			t.Fatalf("untrusted paths reached sender: %+v", options)
		}
		return CodexMessageResult{MessageID: "text", State: "queued"}, nil
	})
	r := httptest.NewRequest(http.MethodPost, "/?thread_id=01a08089-fb23-7d42-bfe5-89a968d8a56a", strings.NewReader(`{"message":"plain text","ImagePaths":["/etc/passwd"],"image_paths":["/etc/passwd"]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
