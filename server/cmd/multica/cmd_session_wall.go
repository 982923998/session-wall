package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon"
)

const defaultSessionWallPort = 19514

var sessionWallCmd = &cobra.Command{
	Use:   "session-wall",
	Short: "Run the focused local Codex conversation card wall",
}

var sessionWallServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the read-only local Codex conversation catalog",
	RunE: func(cmd *cobra.Command, _ []string) error {
		port, _ := cmd.Flags().GetInt("port")
		codexPath, _ := cmd.Flags().GetString("codex-path")
		resolved, err := resolveSessionWallCodexPath(codexPath)
		if err != nil {
			return err
		}

		mux := http.NewServeMux()
		mux.Handle("/codex/threads", daemon.NewCodexThreadCatalogHandler(resolved))
		mux.Handle("/codex/open-thread", daemon.NewCodexOpenThreadHandler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     "running",
				"mode":       "session-wall",
				"codex_path": resolved,
			})
		})

		address := fmt.Sprintf("127.0.0.1:%d", port)
		fmt.Fprintf(cmd.OutOrStdout(), "Session Wall catalog listening on http://%s\n", address)
		server := &http.Server{Addr: address, Handler: mux}
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	},
}

func resolveSessionWallCodexPath(explicit string) (string, error) {
	candidates := []string{
		strings.TrimSpace(explicit),
		strings.TrimSpace(os.Getenv("CODEX_CLI_PATH")),
		"/Applications/ChatGPT.app/Contents/Resources/codex",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if candidate, err := exec.LookPath("codex"); err == nil {
		return candidate, nil
	}
	return "", errors.New("Codex CLI not found; pass --codex-path")
}

func init() {
	sessionWallServeCmd.Flags().Int("port", defaultSessionWallPort, "loopback port for the local catalog")
	sessionWallServeCmd.Flags().String("codex-path", "", "absolute path to the Codex CLI")
	sessionWallCmd.AddCommand(sessionWallServeCmd)
}
