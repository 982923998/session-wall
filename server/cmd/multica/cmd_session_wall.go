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

var sessionWallTaskToolsCmd = &cobra.Command{
	Use:   "task-tools",
	Short: "Serve project-scoped Router task tools over stdio",
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, _ := cmd.Flags().GetString("codex-path")
		parent, _ := cmd.Flags().GetString("parent-thread-id")
		resolved, err := resolveSessionWallCodexPath(path)
		if err != nil {
			return err
		}
		return daemon.ServeCodexTaskTools(resolved, parent, cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var sessionWallServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the read-only local Codex conversation catalog",
	RunE: func(cmd *cobra.Command, _ []string) error {
		port, _ := cmd.Flags().GetInt("port")
		codexPath, _ := cmd.Flags().GetString("codex-path")
		projectsFile, _ := cmd.Flags().GetString("projects-file")
		resolved, err := resolveSessionWallCodexPath(codexPath)
		if err != nil {
			return err
		}

		mux := http.NewServeMux()
		mux.Handle("/codex/threads", daemon.NewCodexThreadCatalogHandlerWithProjects(resolved, projectsFile))
		mux.Handle("/codex/thread-statuses", daemon.NewCodexThreadStatusesHandler())
		mux.Handle("/codex/thread-transcript", daemon.NewCodexThreadTranscriptHandler(resolved))
		mux.Handle("/codex/models", daemon.NewCodexModelsHandler(resolved))
		mux.Handle("/codex/send-message", daemon.NewCodexSendMessageHandler(resolved))
		mux.Handle("/codex/open-thread", daemon.NewCodexOpenThreadHandler())
		mux.Handle("/codex/restore-thread", daemon.NewCodexThreadRestoreHandler(resolved))
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
	sessionWallTaskToolsCmd.Flags().String("codex-path", "", "absolute path to the Codex CLI")
	sessionWallTaskToolsCmd.Flags().String("parent-thread-id", "", "calling Router task ID")
	sessionWallCmd.AddCommand(sessionWallTaskToolsCmd)
	sessionWallServeCmd.Flags().Int("port", defaultSessionWallPort, "loopback port for the local catalog")
	sessionWallServeCmd.Flags().String("codex-path", "", "absolute path to the Codex CLI")
	sessionWallServeCmd.Flags().String("projects-file", "", "JSON file containing the Codex projects to read")
	sessionWallCmd.AddCommand(sessionWallServeCmd)
}
