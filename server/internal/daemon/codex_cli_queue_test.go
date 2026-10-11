package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPublicSenderUsesOnlyOfficialAppForLoadedAndUnloadedTasks(t *testing.T) {
	const id = "01a08089-fb23-7d42-bfe5-89a968d8a56a"
	for _, loaded := range []bool{false, true} {
		t.Run(fmt.Sprint(loaded), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
			lockDir := filepath.Join(home, "thread-writer-locks")
			if err := os.Mkdir(lockDir, 0700); err != nil {
				t.Fatal(err)
			}
			if loaded {
				if err := os.WriteFile(filepath.Join(lockDir, id+".lock"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				"lsof":  "#!/bin/sh\nprintf '123\\n'\n",
				"ps":    "#!/bin/sh\nif [ \"$4\" = ppid= ]; then printf '456\\n'; else printf '/Applications/ChatGPT.app/Contents/MacOS/ChatGPT\\n'; fi\n",
				"open":  "#!/bin/sh\n[ \"$1\" = codex://threads/" + id + " ] || exit 99\n: > \"$CODEX_HOME/thread-writer-locks/" + id + ".lock\"\nprintf 'open\\n' >> \"$CODEX_HOME/calls\"\n",
				"codex": "#!/bin/sh\n[ \"$1\" = queue ] || exit 99\nprintf 'queue\\n' >> \"$CODEX_HOME/calls\"\nprintf 'Queued message 01a0b959-9c3f-7563-9d08-b2e5f0da9ffe for thread " + id + ".\\n'\n",
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			handler := NewCodexSendMessageHandler(filepath.Join(home, "codex"))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/?thread_id="+id, strings.NewReader("hello")))
			var result CodexMessageResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Code != http.StatusAccepted || result.Delivery != "client_queue" || result.State != "queued" {
				t.Fatalf("%d %+v", w.Code, result)
			}
			calls, err := os.ReadFile(filepath.Join(home, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			want := "queue\n"
			if !loaded {
				want = "open\nqueue\n"
			}
			if string(calls) != want {
				t.Fatalf("calls=%q want=%q", calls, want)
			}
		})
	}
}

func TestOfficialAppDoesNotQueueWithoutVerifiedDesktopOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", home+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(home, "open"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "unexpected-queue")
	t.Setenv("CARD_QUEUE_TEST_MARKER", marker)
	executable := filepath.Join(home, "codex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n: > \"$CARD_QUEUE_TEST_MARKER\"\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := sendViaOfficialApp(ctx, executable, home, "01a08089-fb23-7d42-bfe5-89a968d8a56a", "hello", CodexMessageOptions{})
	if err == nil {
		t.Fatal("unverified ownership must fail")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("queue must not be invoked without verified desktop ownership")
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
