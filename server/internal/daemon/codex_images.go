package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const codexImageMax = 8 * 1024 * 1024
const codexImageCountMax = 6
const codexImagesTotalMax = 32 * 1024 * 1024
const codexUploadMax = codexImagesTotalMax + codexMessageTextMax + 128*1024

func codexUploadsDirectory() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "SessionWall", "uploads")
}

// Uploaded names are never used as paths. Keep files for delayed queue consumption and history.
func readCodexImageMessage(w http.ResponseWriter, r *http.Request, root string) (string, []string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, codexUploadMax)
	if err := r.ParseMultipartForm(4 * 1024 * 1024); err != nil {
		return "", nil, fmt.Errorf("图片总大小不能超过 32 MB")
	}
	defer r.MultipartForm.RemoveAll()
	message := strings.TrimSpace(r.FormValue("message"))
	files := r.MultipartForm.File["images"]
	if len(files) == 0 || len(files) > codexImageCountMax {
		return "", nil, fmt.Errorf("请选择 1–6 张图片")
	}
	if len([]byte(message)) > codexMessageTextMax {
		return "", nil, fmt.Errorf("文字内容过长")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", nil, err
	}
	dir := filepath.Join(root, uuid.NewString())
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	paths := make([]string, 0, len(files))
	totalBytes := 0
	for index, header := range files {
		file, err := header.Open()
		if err != nil {
			return "", nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, codexImageMax+1))
		_ = file.Close()
		if err != nil {
			return "", nil, err
		}
		if len(data) > codexImageMax {
			return "", nil, fmt.Errorf("单张图片不能超过 8 MB")
		}
		totalBytes += len(data)
		if totalBytes > codexImagesTotalMax {
			return "", nil, fmt.Errorf("图片总大小不能超过 32 MB")
		}
		ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif"}[http.DetectContentType(data)]
		if ext == "" {
			return "", nil, fmt.Errorf("只支持 PNG、JPEG、WebP 和 GIF 图片")
		}
		path := filepath.Join(dir, fmt.Sprintf("%d.%s", index+1, ext))
		if err := os.WriteFile(path, data, 0600); err != nil {
			return "", nil, err
		}
		paths = append(paths, path)
	}
	if message == "" {
		message = "（图片）"
	}
	keep = true
	return message, paths, nil
}

var codexImageAssetPattern = regexp.MustCompile(`^[0-9a-f-]{36}/[1-6]\.(png|jpg|webp|gif)$`)

func recordCodexImageMessage(root, clientID string, paths []string) error {
	assets := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil || !codexImageAssetPattern.MatchString(filepath.ToSlash(rel)) {
			return fmt.Errorf("invalid image asset")
		}
		assets = append(assets, filepath.ToSlash(rel))
	}
	data, err := json.Marshal(assets)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, clientID+".json"), data, 0600)
}

func codexMessageImageURLs(root, clientID string) []string {
	if !codexThreadIDPattern.MatchString(clientID) {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, clientID+".json"))
	if err != nil {
		return nil
	}
	var assets []string
	if json.Unmarshal(data, &assets) != nil {
		return nil
	}
	urls := []string{}
	for _, asset := range assets {
		if codexImageAssetPattern.MatchString(asset) && len(urls) < codexImageCountMax {
			urls = append(urls, "http://127.0.0.1:19514/codex/attachments/"+asset)
		}
	}
	return urls
}

func codexImageURL(path string) string {
	rel, err := filepath.Rel(codexUploadsDirectory(), path)
	if err != nil || !codexImageAssetPattern.MatchString(filepath.ToSlash(rel)) {
		return ""
	}
	return "http://127.0.0.1:19514/codex/attachments/" + filepath.ToSlash(rel)
}

func NewCodexImageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !localCatalogOrigin(r.Header.Get("Origin")) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "not allowed", http.StatusForbidden)
			return
		}
		asset := strings.TrimPrefix(r.URL.Path, "/codex/attachments/")
		if !codexImageAssetPattern.MatchString(asset) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, filepath.Join(codexUploadsDirectory(), filepath.FromSlash(asset)))
	})
}
