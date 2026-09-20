package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeDesktopOwner(t *testing.T) {
	for _, tc := range []struct {
		name, parent string
		failed, want bool
	}{
		{"desktop", "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT", false, true},
		{"web", "/path/session-wall-server", false, false},
		{"unavailable", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := probeDesktopOwner(context.Background(), "/test/task.lock", func(_ context.Context, command string, args ...string) ([]byte, error) {
				if tc.failed {
					return nil, errors.New("unavailable")
				}
				if command == "lsof" {
					return []byte("123\n"), nil
				}
				if args[3] == "ppid=" {
					return []byte("456\n"), nil
				}
				return []byte(tc.parent + "\n"), nil
			})
			if got != tc.want {
				t.Fatalf("owner=%v", got)
			}
		})
	}
}

func TestDesktopOwnedMessageSkipsIndependentExecutor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
	const id = "01a08089-fb23-7d42-bfe5-89a968d8a56a"
	if err := os.Mkdir(filepath.Join(home, "thread-writer-locks"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"thread-writer-locks/" + id + ".lock": "",
		"lsof":                                "#!/bin/sh\nprintf '123\\n'\n",
		"ps":                                  "#!/bin/sh\nif [ \"$4\" = ppid= ]; then printf '456\\n'; else printf '/Applications/ChatGPT.app/Contents/MacOS/ChatGPT\\n'; fi\n",
		"codex":                               "#!/bin/sh\n[ \"$1\" = queue ] || exit 99\nprintf 'Queued message 01a0b959-9c3f-7563-9d08-b2e5f0da9ffe for thread " + id + ".\\n'\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(home, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	session := newCodexThreadMessageSession(filepath.Join(home, "codex"))
	result, err := session.sendMessage(context.Background(), id, "hello", CodexMessageOptions{})
	if err != nil || result.Delivery != "client_queue" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestCodexCLIQueue(t *testing.T) {
	const thread = "01a08089-fb23-7d42-bfe5-89a968d8a56a"
	const queueID = "01a0b959-9c3f-7563-9d08-b2e5f0da9ffe"
	dir := t.TempDir()
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"accepted", `printf 'Queued message ` + queueID + ` for thread ` + thread + `.\n'`, true},
		{"wrong-thread", `printf 'Queued message ` + queueID + ` for thread wrong.\n'`, false},
		{"unknown-output", `printf 'changed output'`, false},
		{"failure", `exit 1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			script := "#!/bin/sh\n" + `[ "$1" = queue ] && [ "$2" = --thread ] && [ "$3" = ` + thread + ` ] && [ "$4" = --message ] && [ "$5" = 'literal $text; no shell expansion' ] || exit 2` + "\n" + tc.body + "\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			result, err := queueViaCodexCLI(context.Background(), path, dir, thread, "literal $text; no shell expansion", CodexMessageOptions{})
			if (err == nil) != tc.ok {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if tc.ok && (result.State != "queued" || result.Delivery != "client_queue" || result.TurnID != "" || result.MessageID != queueID) {
				t.Fatal(result)
			}
		})
	}
}

func TestCodexCLIQueueModelMismatchDoesNotSubmit(t *testing.T) {
	home := t.TempDir()
	if out, err := exec.Command("sqlite3", filepath.Join(home, "state_5.sqlite"), "CREATE TABLE threads(id TEXT,model TEXT,reasoning_effort TEXT); INSERT INTO threads VALUES('t','model-a','high');").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	_, err := queueViaCodexCLI(context.Background(), "/nonexistent/codex", home, "t", "message", CodexMessageOptions{Model: "model-b"})
	if err == nil || !strings.Contains(err.Error(), "设置保持一致") {
		t.Fatal(err)
	}
	if err := checkQueuedModelSettings(context.Background(), home, "t", CodexMessageOptions{Model: "model-a", ReasoningEffort: "high"}); err != nil {
		t.Fatal(err)
	}
}
