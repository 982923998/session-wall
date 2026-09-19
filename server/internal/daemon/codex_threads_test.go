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
			"status": {"type": "idle"},
			"gitInfo": {"branch": "main"}
		}],
		"nextCursor": null
	}`)

	threads, cursor, err := decodeCodexThreadListResult(raw)
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
	if got.Branch != "main" || got.RecencyAt != 30 || got.Pinned || cursor != nil {
		t.Fatalf("thread metadata = %+v", got)
	}
}

func TestCodexThreadSourceIsDelegated(t *testing.T) {
	if !codexThreadSourceIsDelegated(`{"subagent":{"thread_spawn":{"parent_thread_id":"root"}}}`) {
		t.Fatal("subagent object source must be delegated")
	}
	if !codexThreadSourceIsDelegated(`"subagent"`) {
		t.Fatal("subagent string source must be delegated")
	}
	if codexThreadSourceIsDelegated(`{"user":{}}`) {
		t.Fatal("user source must not be delegated")
	}
}

func TestDecodeCodexModelListResult(t *testing.T) {
	raw := json.RawMessage(`{
		"data": [{
			"id": "catalog-gpt-5.6-sol",
			"model": "gpt-5.6-sol",
			"displayName": "GPT-5.6-Sol",
			"description": "Latest frontier agentic coding model.",
			"hidden": false,
			"supportedReasoningEfforts": [
				{"reasoningEffort": "low", "description": "Fast responses"},
				{"reasoningEffort": "ultra", "description": "Automatic task delegation"}
			],
			"defaultReasoningEffort": "low",
			"isDefault": true
		}, {
			"id": "hidden-model",
			"model": "hidden-model",
			"displayName": "Hidden",
			"hidden": true,
			"supportedReasoningEfforts": [],
			"defaultReasoningEffort": "medium",
			"isDefault": false
		}],
		"nextCursor": null
	}`)

	models, cursor, err := decodeCodexModelListResult(raw)
	if err != nil {
		t.Fatalf("decodeCodexModelListResult() error = %v", err)
	}
	if cursor != nil || len(models) != 1 {
		t.Fatalf("models = %#v, cursor = %#v", models, cursor)
	}
	got := models[0]
	if got.ID != "gpt-5.6-sol" || got.Name != "GPT-5.6-Sol" || !got.Default {
		t.Fatalf("model = %+v", got)
	}
	if got.DefaultReasoningEffort != "low" || len(got.ReasoningEfforts) != 2 || got.ReasoningEfforts[1].Value != "ultra" {
		t.Fatalf("reasoning catalog = %+v", got)
	}
}

func TestReadCodexModelsCache(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "models_cache.json")
	if err := os.WriteFile(filename, []byte(`{
		"models": [{
			"slug": "gpt-5.6-sol",
			"display_name": "GPT-5.6-Sol",
			"description": "Latest frontier model.",
			"visibility": "list",
			"default_reasoning_level": "low",
			"supported_reasoning_levels": [
				{"effort": "low", "description": "Fast"},
				{"effort": "ultra", "description": "Maximum"}
			]
		}, {
			"slug": "hidden-model",
			"display_name": "Hidden",
			"visibility": "hide"
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	models, err := readCodexModelsCache(filename)
	if err != nil {
		t.Fatalf("readCodexModelsCache() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.6-sol" || !models[0].Default {
		t.Fatalf("models = %+v", models)
	}
	if models[0].DefaultReasoningEffort != "low" || models[0].ReasoningEfforts[1].Value != "ultra" {
		t.Fatalf("reasoning catalog = %+v", models[0])
	}
}

func TestCodexThreadDisplayNameRecoversLegacyTitlesAndDelegations(t *testing.T) {
	if got := codexThreadDisplayName("", "implementation · 01-v3", ""); got != "implementation · 01-v3" {
		t.Fatalf("formal title fallback = %q", got)
	}
	delegation := `<codex_delegation>
  <input>你是 AI+ASD 项目的 neuro-ai-analysis-implementation role agent。</input>
  <objective / scope: 03；修改文档
</codex_delegation>`
	if got := codexThreadDisplayName("", delegation, delegation); got != "implementation · 03" {
		t.Fatalf("delegation scope fallback = %q", got)
	}
}

func TestCodexThreadRoleRecoversLegacyRouterTitles(t *testing.T) {
	if got := codexThreadRole("", "你是本项目的router agent", ""); got != "router" {
		t.Fatalf("router title fallback = %q", got)
	}
	if got := codexThreadRole("", "", "<codex_delegation> neuro-ai-research-router </codex_delegation>"); got != "router" {
		t.Fatalf("router delegation fallback = %q", got)
	}
	if got := codexThreadRole("implementation", "router agent", ""); got != "implementation" {
		t.Fatalf("explicit agent role = %q", got)
	}
}

func TestListCodexThreadStatusesFromSQLite(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "thread_history_1.sqlite")
	setup := `CREATE TABLE thread_turns (
  thread_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  rollout_ordinal INTEGER NOT NULL,
  status TEXT NOT NULL,
  started_at INTEGER
);
INSERT INTO thread_turns VALUES
  ('active-thread', 'old-turn', 1, 'completed', 10),
  ('active-thread', 'new-turn', 2, 'inProgress', 20),
  ('completed-thread', 'done-turn', 1, 'completed', 30),
  ('interrupted-thread', 'stopped-turn', 1, 'interrupted', 40);`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare thread history database: %v: %s", err, output)
	}

	statuses, err := listCodexThreadStatusesFromSQLite(context.Background(), codexHome)
	if err != nil {
		t.Fatalf("listCodexThreadStatusesFromSQLite() error = %v", err)
	}
	if statuses["active-thread"] != "active" {
		t.Fatalf("active status = %q", statuses["active-thread"])
	}
	if statuses["completed-thread"] != "completed" {
		t.Fatalf("completed status = %q", statuses["completed-thread"])
	}
	if statuses["interrupted-thread"] != "interrupted" {
		t.Fatalf("interrupted status = %q", statuses["interrupted-thread"])
	}
}

func TestListCodexArchivedThreadIDsFromSQLite(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "state_5.sqlite")
	setup := `CREATE TABLE threads (id TEXT NOT NULL, archived INTEGER NOT NULL);
INSERT INTO threads VALUES
  ('visible-thread', 0),
  ('archived-b', 1),
  ('archived-a', 1);`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare Codex state database: %v: %s", err, output)
	}

	ids, err := listCodexArchivedThreadIDsFromSQLite(context.Background(), codexHome)
	if err != nil {
		t.Fatalf("listCodexArchivedThreadIDsFromSQLite() error = %v", err)
	}
	if strings.Join(ids, ",") != "archived-a,archived-b" {
		t.Fatalf("archived ids = %#v", ids)
	}
}

func TestListCodexArchivedProjectThreadsFromSQLiteBackfillsNamedCards(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "state_5.sqlite")
	setup := `CREATE TABLE threads (
  id TEXT, name TEXT, title TEXT, preview TEXT, cwd TEXT, git_branch TEXT,
  git_origin_url TEXT, model TEXT, reasoning_effort TEXT, created_at INTEGER,
  updated_at INTEGER, recency_at INTEGER, agent_role TEXT, agent_nickname TEXT,
  source TEXT, is_pinned INTEGER, archived INTEGER
);
INSERT INTO threads VALUES
  ('named-archived', 'method · 1.3.3', '', '', '/missing/worktree/V1', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 3, '', '', 'appServer', 0, 1),
  ('unnamed-archived', NULL, 'delegation', '旧的无名任务', '/missing/worktree/V1', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 4, '', '', 'appServer', 0, 1),
  ('named-live', 'implementation · 05', 'delegation', '仍然可见', '/missing/worktree/V1', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 5, '', '', 'appServer', 0, 0),
  ('other-project', 'writing · Methods', 'delegation', '其他项目', '/missing/other', '',
   'https://github.com/example/other.git', 'gpt', 'high', 1, 2, 6, '', '', 'appServer', 0, 1);`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare archived catalog database: %v: %s", err, output)
	}
	root := "/projects/AI+ASD/V1"
	threads, err := listCodexArchivedProjectThreadsFromSQLite(
		context.Background(), codexHome,
		[]CodexProjectSummary{{ID: "asd", Name: "AI+ASD", Roots: []string{root}}},
		map[string]string{"https://github.com/example/asd.git": root},
	)
	if err != nil {
		t.Fatalf("listCodexArchivedProjectThreadsFromSQLite() error = %v", err)
	}
	if len(threads) != 1 || threads[0].ID != "named-archived" || threads[0].ProjectRoot != root {
		t.Fatalf("archived threads = %+v", threads)
	}
	if output, err := exec.Command("sqlite3", database, "DELETE FROM threads;").CombinedOutput(); err != nil {
		t.Fatalf("clear archived catalog database: %v: %s", err, output)
	}
	empty, err := listCodexArchivedProjectThreadsFromSQLite(
		context.Background(), codexHome,
		[]CodexProjectSummary{{ID: "asd", Name: "AI+ASD", Roots: []string{root}}},
		map[string]string{"https://github.com/example/asd.git": root},
	)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty archived threads = %+v, error = %v", empty, err)
	}
}

func TestListCodexCatalogFromSQLiteKeepsRestoredNamedTaskWithMissingWorktree(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "state_5.sqlite")
	root := "/projects/AI+ASD/V1"
	setup := `CREATE TABLE threads (
  id TEXT, name TEXT, title TEXT, preview TEXT, cwd TEXT, git_branch TEXT,
  git_origin_url TEXT, model TEXT, reasoning_effort TEXT, created_at INTEGER,
  updated_at INTEGER, recency_at INTEGER, agent_role TEXT, agent_nickname TEXT,
  source TEXT, is_pinned INTEGER, archived INTEGER
);
INSERT INTO threads VALUES
  ('direct-live', 'implementation · 05', '', NULL, '` + root + `', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 3, '', '', 'appServer', 0, 0),
  ('restored-live', 'method · 1.3.3', '', '', '/missing/worktree/V1', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 4, '', '', 'appServer', 0, 0),
  ('unnamed-live', NULL, '', '内部任务', '/missing/worktree/V1', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 5, '', '', 'appServer', 0, 0),
  ('archived-card', 'writing · Methods', '', '归档任务', '` + root + `', '',
   'https://github.com/example/asd.git', 'gpt', 'high', 1, 2, 6, '', '', 'appServer', 0, 1);`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare Codex catalog database: %v: %s", err, output)
	}

	catalog, err := listCodexCatalogFromSQLite(context.Background(), codexHome, []CodexProjectSummary{{
		ID: "asd", Name: "AI+ASD", Roots: []string{root},
	}})
	if err != nil {
		t.Fatalf("listCodexCatalogFromSQLite() error = %v", err)
	}
	if len(catalog.Threads) != 2 || catalog.Threads[0].ID != "restored-live" || catalog.Threads[1].ID != "direct-live" {
		t.Fatalf("active threads = %#v", catalog.Threads)
	}
	if len(catalog.ArchivedThreads) != 1 || catalog.ArchivedThreads[0].ID != "archived-card" {
		t.Fatalf("archived threads = %#v", catalog.ArchivedThreads)
	}
}

func TestWebMessageBusyDoesNotQueue(t *testing.T) {
	s := newCodexThreadMessageSession("/nonexistent/codex")
	s.executor.mu.Lock()
	defer s.executor.mu.Unlock()
	result, err := s.sendMessage(context.Background(), "thread", "hello", CodexMessageOptions{})
	if err == nil || result.MessageID != "" || result.State == "queued" {
		t.Fatalf("busy message = %+v, error = %v", result, err)
	}
}

func TestWebTurnInterrupt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var input strings.Builder
		reply := `{"id":"wall-interrupt","result":{}}`
		if fail {
			reply = `{"id":"wall-interrupt","error":{"message":"denied"}}`
		}
		client := newCodexCatalogClient(&input, strings.NewReader(reply+"\n"+`{"method":"turn/completed","params":{"turn":{"id":"turn-1"}}}`+"\n"))
		s := &codexCatalogSession{activeTurn: "turn-1", activeClient: client}
		go client.drainTurn("turn-1")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := s.interrupt(ctx, "thread-1")
		cancel()
		if (err != nil) != fail {
			t.Fatalf("interrupt error = %v, fail = %v", err, fail)
		}
		var request struct {
			Method string
			Params struct {
				ThreadID string
				TurnID   string
			}
		}
		if err := json.Unmarshal([]byte(input.String()), &request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "turn/interrupt" || request.Params.ThreadID != "thread-1" || request.Params.TurnID != "turn-1" {
			t.Fatalf("wrong interrupt target: %+v", request)
		}
	}
}

func TestCodexTranscriptPreservesClientMessageIdentity(t *testing.T) {
	item, ok := normalizeCodexTranscriptItem(json.RawMessage(`{"type":"userMessage","id":"persisted-id","clientId":"web-message-id","content":[{"type":"text","text":"hello"}]}`))
	if !ok || item.ID != "persisted-id" || item.ClientID != "web-message-id" || item.Text != "hello" {
		t.Fatalf("message identity lost: %+v", item)
	}
}

func TestReadCodexThreadTranscriptFromSQLite(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "thread_history_1.sqlite")
	setup := `CREATE TABLE thread_turns (
  thread_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  rollout_ordinal INTEGER NOT NULL,
  status TEXT NOT NULL
);
CREATE TABLE thread_items (
  thread_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  rollout_ordinal INTEGER NOT NULL,
  item_json TEXT NOT NULL
);
INSERT INTO thread_turns VALUES
  ('thread-1', 'old-turn', 1, 'completed'),
  ('thread-1', 'new-turn', 2, 'inProgress');
INSERT INTO thread_items VALUES
  ('thread-1', 'old-turn', 'user-1', 1, '{"type":"userMessage","id":"user-1","content":[{"type":"text","text":"开始"}]}'),
  ('thread-1', 'new-turn', 'assistant-1', 2, '{"type":"agentMessage","id":"assistant-1","text":"正在处理","phase":"commentary"}');`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare transcript database: %v: %s", err, output)
	}

	transcript, err := readCodexThreadTranscriptFromSQLite(context.Background(), codexHome, "thread-1")
	if err != nil {
		t.Fatalf("readCodexThreadTranscriptFromSQLite() error = %v", err)
	}
	if transcript.Status != "active" || len(transcript.Items) != 2 {
		t.Fatalf("transcript = %+v", transcript)
	}
	if transcript.Items[0].Kind != "user" || transcript.Items[1].Phase != "commentary" {
		t.Fatalf("items = %+v", transcript.Items)
	}
}

func TestListAllCodexThreadsFollowsPagination(t *testing.T) {
	calls := 0
	threads, err := listAllCodexThreads(func(method string, params any) (json.RawMessage, error) {
		calls++
		if method != "thread/list" {
			t.Fatalf("method = %q", method)
		}
		cursor := params.(map[string]any)["cursor"]
		if calls == 1 {
			if cursor != (*string)(nil) {
				t.Fatalf("first cursor = %#v", cursor)
			}
			return json.RawMessage(`{"data":[{"id":"thread-1"}],"nextCursor":"page-2"}`), nil
		}
		if got := *(cursor.(*string)); got != "page-2" {
			t.Fatalf("second cursor = %q", got)
		}
		return json.RawMessage(`{"data":[{"id":"thread-2"}],"nextCursor":null}`), nil
	})
	if err != nil {
		t.Fatalf("listAllCodexThreads() error = %v", err)
	}
	if calls != 2 || len(threads) != 2 || threads[1].ID != "thread-2" {
		t.Fatalf("calls = %d, threads = %+v", calls, threads)
	}
}

func TestCodexTranscriptIntermediateActivity(t *testing.T) {
	for _, tc := range []struct{ raw, kind, text string }{
		{`{"type":"reasoning","id":"r","summary":[],"content":["private reasoning must not be displayed"]}`, "activity", "此阶段没有可显示的公开摘要。"},
		{`{"type":"reasoning","id":"r","summary":["公开进度"],"content":["private"]}`, "activity", "公开进度"},
		{`{"type":"mcpToolCall","id":"t","server":"test","tool":"read","status":"completed","result":{"content":[{"type":"text","text":"done"}]}}`, "tool", "done"},
		{`{"type":"fileChange","id":"f","status":"completed","changes":[{"path":"docs/method.md","diff":"+updated"}]}`, "tool", "docs/method.md\n+updated"},
	} {
		item, ok := normalizeCodexTranscriptItem(json.RawMessage(tc.raw))
		if !ok || item.Kind != tc.kind || item.Text != tc.text {
			t.Fatalf("normalized item = %+v, ok=%v", item, ok)
		}
	}
}

func TestReadCodexThreadTranscriptNormalizesRecentTurns(t *testing.T) {
	transcript, err := readCodexThreadTranscript(func(method string, params any) (json.RawMessage, error) {
		if method != "thread/turns/list" {
			t.Fatalf("method = %q", method)
		}
		values := params.(map[string]any)
		if values["threadId"] != "thread-1" || values["itemsView"] != "full" || values["limit"] != codexTranscriptLimit {
			t.Fatalf("params = %#v", values)
		}
		return json.RawMessage(`{"data":[
			{"status":"inProgress","items":[
				{"type":"agentMessage","id":"assistant-1","text":"正在处理","phase":"commentary"},
				{"type":"commandExecution","id":"tool-1","command":"go test ./...","aggregatedOutput":"ok","status":"completed"}
			]},
			{"status":"completed","items":[
				{"type":"userMessage","id":"user-1","content":[{"type":"text","text":"开始测试"}]},
				{"type":"reasoning","id":"reasoning-1","summary":["检查项目"]}
			]}
		]}`), nil
	}, "thread-1")
	if err != nil {
		t.Fatalf("readCodexThreadTranscript() error = %v", err)
	}
	if transcript.Status != "active" || len(transcript.Items) != 4 {
		t.Fatalf("transcript = %+v", transcript)
	}
	if transcript.Items[0].Kind != "user" || transcript.Items[0].Text != "开始测试" {
		t.Fatalf("first item = %+v", transcript.Items[0])
	}
	if transcript.Items[2].Kind != "assistant" || transcript.Items[3].Kind != "tool" || transcript.Items[3].Text != "ok" {
		t.Fatalf("latest items = %+v", transcript.Items[2:])
	}
}

func TestTrimCodexTranscriptItemsKeepsUserMessagesAndLatestActivity(t *testing.T) {
	items := []CodexTranscriptItem{{ID: "user-1", Kind: "user", Text: "开始"}}
	for index := 0; index < codexTranscriptItemMax+10; index++ {
		items = append(items, CodexTranscriptItem{ID: fmt.Sprintf("tool-%d", index), Kind: "tool"})
	}
	trimmed := trimCodexTranscriptItems(items)
	if len(trimmed) != codexTranscriptItemMax {
		t.Fatalf("len(trimmed) = %d", len(trimmed))
	}
	if trimmed[0].ID != "user-1" || trimmed[len(trimmed)-1].ID != fmt.Sprintf("tool-%d", codexTranscriptItemMax+9) {
		t.Fatalf("trimmed endpoints = %q, %q", trimmed[0].ID, trimmed[len(trimmed)-1].ID)
	}
}

func TestListSelectedCodexThreadsFiltersAtSourceAndMarksProjectRoot(t *testing.T) {
	cwdRoots := map[string]string{
		"/Users/test/.codex/worktrees/a/V1": "/projects/AI+ASD/V1",
		"/projects/card/session-wall":       "/projects/card",
	}
	var gotCWDs []string
	threads, err := listSelectedCodexThreads(func(method string, params any) (json.RawMessage, error) {
		if method != "thread/list" {
			t.Fatalf("method = %q", method)
		}
		gotCWDs = params.(map[string]any)["cwd"].([]string)
		return json.RawMessage(`{"data":[
			{"id":"method-03","cwd":"/Users/test/.codex/worktrees/a/V1"},
			{"id":"card-1","cwd":"/projects/card/session-wall"}
		],"nextCursor":null}`), nil
	}, cwdRoots)
	if err != nil {
		t.Fatalf("listSelectedCodexThreads() error = %v", err)
	}
	if len(gotCWDs) != 2 {
		t.Fatalf("cwd filter = %v", gotCWDs)
	}
	if len(threads) != 2 || threads[0].ProjectRoot != "/projects/AI+ASD/V1" || threads[1].ProjectRoot != "/projects/card" {
		t.Fatalf("threads = %+v", threads)
	}
}

func TestListSelectedCodexThreadsDoesNotFallBackToAllThreads(t *testing.T) {
	threads, err := listSelectedCodexThreads(func(string, any) (json.RawMessage, error) {
		t.Fatal("thread/list must not run when no selected project has sessions")
		return nil, nil
	}, map[string]string{})
	if err != nil || len(threads) != 0 {
		t.Fatalf("threads = %+v, error = %v", threads, err)
	}
}

func TestReadCodexProjectSelection(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(filename, []byte(`{"projects":[
		{"id":"project-asd","name":"AI+ASD","position":0,"roots":["/projects/V1"]},
		{"id":"project-asd","name":"duplicate","roots":["/duplicate"]},
		{"id":"","name":"invalid","roots":["/invalid"]}
	]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := readCodexProjectSelection(filename)
	if err != nil {
		t.Fatalf("readCodexProjectSelection() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "AI+ASD" || projects[0].Roots[0] != "/projects/V1" {
		t.Fatalf("projects = %+v", projects)
	}
}

func TestDecodeCodexProjectListResult(t *testing.T) {
	raw := json.RawMessage(`{
		"data":[{"id":"project-1","name":"AI+ASD","position":3,"roots":[{"path":"/projects/V1"}]}],
		"nextCursor":"next"
	}`)
	projects, cursor, err := decodeCodexProjectListResult(raw)
	if err != nil {
		t.Fatalf("decodeCodexProjectListResult() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "AI+ASD" || projects[0].Roots[0] != "/projects/V1" {
		t.Fatalf("projects = %+v", projects)
	}
	if cursor == nil || *cursor != "next" {
		t.Fatalf("cursor = %#v", cursor)
	}
}

func TestProjectRootFromGitCommonDir(t *testing.T) {
	got := projectRootFromGitCommonDir("/projects/AI+ASD/V1/.git\n")
	if got != "/projects/AI+ASD/V1" {
		t.Fatalf("project root = %q", got)
	}
	if got := projectRootFromGitCommonDir("/projects/bare.git"); got != "" {
		t.Fatalf("bare project root = %q, want empty", got)
	}
}

func TestProjectRootFromWorktreeMetadata(t *testing.T) {
	cwd := t.TempDir()
	metadata := "gitdir: /projects/AI+ASD/V1/.git/worktrees/V112\n"
	if err := os.WriteFile(filepath.Join(cwd, ".git"), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := projectRootFromWorktreeMetadata(cwd); got != "/projects/AI+ASD/V1" {
		t.Fatalf("project root = %q", got)
	}
}

func TestMarkCodexThreadParentsFromSQLite(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "state_5.sqlite")
	setup := `CREATE TABLE thread_spawn_edges (
  parent_thread_id TEXT NOT NULL,
  child_thread_id TEXT NOT NULL PRIMARY KEY,
  status TEXT NOT NULL
);
INSERT INTO thread_spawn_edges VALUES
  ('root-thread', 'child-open', 'open'),
  ('root-thread', 'child-closed', 'closed');`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare spawn-edge database: %v: %s", err, output)
	}
	threads := []CodexThreadSummary{
		{ID: "root-thread"},
		{ID: "child-open"},
		{ID: "child-closed"},
	}

	markCodexThreadParents(context.Background(), codexHome, threads)

	if threads[0].ParentThreadID != "" {
		t.Fatalf("root parent = %q", threads[0].ParentThreadID)
	}
	if threads[1].ParentThreadID != "root-thread" || threads[2].ParentThreadID != "root-thread" {
		t.Fatalf("threads = %+v", threads)
	}
}

func TestMarkCodexLatestTurnOriginsFromSQLite(t *testing.T) {
	codexHome := t.TempDir()
	database := filepath.Join(codexHome, "thread_history_1.sqlite")
	setup := `CREATE TABLE thread_turns (
  thread_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  rollout_ordinal INTEGER NOT NULL,
  status TEXT NOT NULL,
  started_at INTEGER,
  first_user_item_id TEXT,
  PRIMARY KEY (thread_id, turn_id)
);
CREATE TABLE thread_items (
  thread_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  rollout_ordinal INTEGER NOT NULL,
  item_json TEXT NOT NULL,
  item_type TEXT NOT NULL,
  PRIMARY KEY (thread_id, turn_id, item_id)
);
INSERT INTO thread_turns VALUES
  ('direct-thread', 'direct-turn', 1, 'completed', 10, 'direct-user'),
  ('called-thread', 'called-turn', 1, 'completed', 20, 'called-user'),
  ('reused-thread', 'old-called-turn', 1, 'completed', 30, 'old-called-user'),
  ('reused-thread', 'new-direct-turn', 2, 'completed', 40, 'new-direct-user');
INSERT INTO thread_items VALUES
  ('direct-thread', 'direct-turn', 'direct-user', 1, '{"type":"userMessage","id":"direct-user","content":[{"type":"text","text":"请直接处理"}]}', 'userMessage'),
  ('called-thread', 'called-turn', 'called-user', 1, '{"type":"userMessage","id":"called-user","content":[{"type":"text","text":"<codex_delegation>\n<source_thread_id>root-thread</source_thread_id>\n<input>请协助处理</input>\n</codex_delegation>"}]}', 'userMessage'),
  ('reused-thread', 'old-called-turn', 'old-called-user', 1, '{"type":"userMessage","id":"old-called-user","content":[{"type":"text","text":"<codex_delegation>\n<source_thread_id>root-thread</source_thread_id>\n<input>旧调用</input>\n</codex_delegation>"}]}', 'userMessage'),
  ('reused-thread', 'new-direct-turn', 'new-direct-user', 2, '{"type":"userMessage","id":"new-direct-user","content":[{"type":"text","text":"现在由用户直接继续"}]}', 'userMessage');`
	if output, err := exec.Command("sqlite3", database, setup).CombinedOutput(); err != nil {
		t.Fatalf("prepare turn delegation database: %v: %s", err, output)
	}
	threads := []CodexThreadSummary{
		{ID: "direct-thread", LatestTurnOrigin: "delegated"},
		{ID: "called-thread"},
		{ID: "reused-thread", LatestTurnOrigin: "delegated"},
	}

	markCodexLatestTurnOrigins(context.Background(), codexHome, threads)

	if threads[0].LatestTurnOrigin != "direct" {
		t.Fatalf("direct latest turn origin = %q", threads[0].LatestTurnOrigin)
	}
	if threads[1].LatestTurnOrigin != "delegated" {
		t.Fatalf("called latest turn origin = %q", threads[1].LatestTurnOrigin)
	}
	if threads[2].LatestTurnOrigin != "direct" {
		t.Fatalf("reused latest turn origin = %q", threads[2].LatestTurnOrigin)
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
	handler := codexThreadsHandler(func(context.Context) (CodexCatalog, error) {
		return CodexCatalog{
			Threads:         []CodexThreadSummary{{ID: "thread-1", Name: "Existing conversation"}},
			ArchivedThreads: []CodexThreadSummary{{ID: "thread-2", Name: "method · 1.3.3"}},
			Projects:        []CodexProjectSummary{{ID: "project-1", Name: "AI+ASD", Roots: []string{"/projects/V1"}}},
		}, nil
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
		Threads         []CodexThreadSummary  `json:"threads"`
		ArchivedThreads []CodexThreadSummary  `json:"archived_threads"`
		Projects        []CodexProjectSummary `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Threads) != 1 || response.Threads[0].ID != "thread-1" ||
		len(response.ArchivedThreads) != 1 || response.ArchivedThreads[0].ID != "thread-2" ||
		response.Projects[0].Name != "AI+ASD" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCodexModelsHandlerReturnsRuntimeCatalog(t *testing.T) {
	handler := codexModelsHandler(func(context.Context) ([]CodexModelSummary, error) {
		return []CodexModelSummary{{
			ID:                     "gpt-5.6-sol",
			Name:                   "GPT-5.6-Sol",
			DefaultReasoningEffort: "high",
			ReasoningEfforts:       []CodexReasoningEffort{{Value: "high"}, {Value: "ultra"}},
			Default:                true,
		}}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/codex/models", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Models []CodexModelSummary `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Models) != 1 || response.Models[0].ID != "gpt-5.6-sol" || response.Models[0].ReasoningEfforts[1].Value != "ultra" {
		t.Fatalf("models = %+v", response.Models)
	}
}

func TestCodexThreadStatusesHandlerReturnsStatusMap(t *testing.T) {
	handler := codexThreadStatusesHandler(func(context.Context) (map[string]string, error) {
		return map[string]string{"thread-1": "active", "thread-2": "completed"}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/codex/thread-statuses", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Statuses map[string]string `json:"statuses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Statuses["thread-1"] != "active" || response.Statuses["thread-2"] != "completed" {
		t.Fatalf("statuses = %#v", response.Statuses)
	}
}

func TestCodexThreadStatusesHandlerReturnsArchivedIDs(t *testing.T) {
	handler := codexThreadStatusesHandler(
		func(context.Context) (map[string]string, error) {
			return map[string]string{"thread-1": "completed"}, nil
		},
		func(context.Context) ([]string, error) {
			return []string{"thread-2"}, nil
		},
	)
	req := httptest.NewRequest(http.MethodGet, "/codex/thread-statuses", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ArchivedThreadIDs []string `json:"archived_thread_ids"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || strings.Join(response.ArchivedThreadIDs, ",") != "thread-2" {
		t.Fatalf("response = %q, err = %v", rec.Body.String(), err)
	}
}

func TestCodexThreadStatusesHandlerFiltersToRequestedCards(t *testing.T) {
	visibleID := "019ed804-66d5-7361-b92a-996db5525f27"
	archivedID := "019f351f-9a67-76e0-8ffe-34fdd9275067"
	handler := codexThreadStatusesHandler(
		func(context.Context) (map[string]string, error) {
			return map[string]string{visibleID: "completed", "unrequested": "active"}, nil
		},
		func(context.Context) ([]string, error) {
			return []string{archivedID, "unrequested"}, nil
		},
	)
	req := httptest.NewRequest(http.MethodGet,
		"/codex/thread-statuses?thread_id="+visibleID+"&thread_id="+archivedID, nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Statuses          map[string]string `json:"statuses"`
		ArchivedThreadIDs []string          `json:"archived_thread_ids"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Statuses) != 1 || response.Statuses[visibleID] != "completed" {
		t.Fatalf("statuses = %#v", response.Statuses)
	}
	if strings.Join(response.ArchivedThreadIDs, ",") != archivedID {
		t.Fatalf("archived ids = %#v", response.ArchivedThreadIDs)
	}
}

func TestUnarchiveCodexThreadUsesProtocol(t *testing.T) {
	result, err := unarchiveCodexThread(func(method string, params any) (json.RawMessage, error) {
		if method != "thread/unarchive" {
			t.Fatalf("method = %q", method)
		}
		values := params.(map[string]any)
		if values["threadId"] != "019ed804-66d5-7361-b92a-996db5525f27" {
			t.Fatalf("params = %#v", values)
		}
		return json.RawMessage(`{"thread":{"id":"019ed804-66d5-7361-b92a-996db5525f27","cwd":"/projects/asd"}}`), nil
	}, "019ed804-66d5-7361-b92a-996db5525f27")
	if err != nil {
		t.Fatalf("unarchiveCodexThread() error = %v", err)
	}
	if result.ThreadID != "019ed804-66d5-7361-b92a-996db5525f27" || result.CWD != "/projects/asd" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCodexRestoreThreadHandlerRestoresValidatedTask(t *testing.T) {
	var restored string
	handler := codexRestoreThreadHandler(func(_ context.Context, threadID string) (CodexThreadRestoreResult, error) {
		restored = threadID
		return CodexThreadRestoreResult{ThreadID: threadID, CWD: "/projects/asd"}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/codex/restore-thread?thread_id=019ed804-66d5-7361-b92a-996db5525f27", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || restored != "019ed804-66d5-7361-b92a-996db5525f27" {
		t.Fatalf("status = %d, restored = %q, body = %s", rec.Code, restored, rec.Body.String())
	}
	var response CodexThreadRestoreResult
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.ThreadID != restored {
		t.Fatalf("response = %q, err = %v", rec.Body.String(), err)
	}
}

func TestCodexRestoreThreadHandlerRejectsInvalidRequest(t *testing.T) {
	handler := codexRestoreThreadHandler(func(context.Context, string) (CodexThreadRestoreResult, error) {
		t.Fatal("restorer must not run")
		return CodexThreadRestoreResult{}, nil
	})
	tests := []struct {
		name   string
		method string
		path   string
		origin string
		want   int
	}{
		{name: "invalid id", method: http.MethodPost, path: "/codex/restore-thread?thread_id=bad", origin: "http://localhost:3000", want: http.StatusBadRequest},
		{name: "wrong method", method: http.MethodGet, path: "/codex/restore-thread?thread_id=019ed804-66d5-7361-b92a-996db5525f27", origin: "http://localhost:3000", want: http.StatusMethodNotAllowed},
		{name: "foreign origin", method: http.MethodPost, path: "/codex/restore-thread?thread_id=019ed804-66d5-7361-b92a-996db5525f27", origin: "https://example.com", want: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCodexThreadTranscriptHandlerReturnsConversation(t *testing.T) {
	handler := codexThreadTranscriptHandler(func(_ context.Context, threadID string) (CodexThreadTranscript, error) {
		return CodexThreadTranscript{
			ThreadID: threadID,
			Status:   "active",
			Items:    []CodexTranscriptItem{{ID: "message-1", Kind: "assistant", Text: "进行中"}},
		}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/codex/thread-transcript?thread_id=019ed804-66d5-7361-b92a-996db5525f27", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response CodexThreadTranscript
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "active" || len(response.Items) != 1 || response.Items[0].Text != "进行中" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCodexThreadTranscriptHandlerRejectsInvalidID(t *testing.T) {
	handler := codexThreadTranscriptHandler(func(context.Context, string) (CodexThreadTranscript, error) {
		t.Fatal("reader must not run")
		return CodexThreadTranscript{}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/codex/thread-transcript?thread_id=not-a-thread", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestQueueCodexThreadMessageUsesWarmAppServer(t *testing.T) {
	var methods []string
	call := func(method string, params any) (json.RawMessage, error) {
		methods = append(methods, method)
		payload, ok := params.(map[string]any)
		if !ok || payload["threadId"] != "019ed804-66d5-7361-b92a-996db5525f27" {
			t.Fatalf("%s params = %#v", method, params)
		}
		if method != "thread/queue/add" {
			t.Fatalf("unexpected method %q", method)
		}
		if payload["clientUserMessageId"] != "client-message-1" {
			t.Fatalf("client message id = %#v", payload["clientUserMessageId"])
		}
		input, ok := payload["input"].([]map[string]any)
		if !ok || len(input) != 1 || input[0]["type"] != "text" || input[0]["text"] != "继续检查" {
			t.Fatalf("queue input = %#v", payload["input"])
		}
		return json.RawMessage(`{"queuedSubmission":{"id":"submission-1"}}`), nil
	}

	submissionID, err := queueCodexThreadMessage(call, "019ed804-66d5-7361-b92a-996db5525f27", "继续检查", "client-message-1")
	if err != nil {
		t.Fatalf("queueCodexThreadMessage() error = %v", err)
	}
	if submissionID != "submission-1" || strings.Join(methods, ",") != "thread/queue/add" {
		t.Fatalf("submissionID = %q, methods = %v", submissionID, methods)
	}
}

func TestListCodexQueuedMessageIDsPreservesQueueOrder(t *testing.T) {
	ids, err := listCodexQueuedMessageIDs(func(method string, params any) (json.RawMessage, error) {
		if method != "thread/queue/list" {
			t.Fatalf("method = %q", method)
		}
		payload := params.(map[string]any)
		if payload["threadId"] != "019ed804-66d5-7361-b92a-996db5525f27" || payload["limit"] != 100 {
			t.Fatalf("params = %#v", payload)
		}
		return json.RawMessage(`{"data":[{"id":"submission-1"},{"id":"submission-2"}],"nextCursor":null}`), nil
	}, "019ed804-66d5-7361-b92a-996db5525f27")
	if err != nil {
		t.Fatalf("listCodexQueuedMessageIDs() error = %v", err)
	}
	if strings.Join(ids, ",") != "submission-1,submission-2" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestStartCodexQueuedMessageAppliesModelAndEffort(t *testing.T) {
	var methods []string
	call := func(method string, params any) (json.RawMessage, error) {
		methods = append(methods, method)
		payload := params.(map[string]any)
		if payload["threadId"] != "019f351f-9a67-76e0-8ffe-34fdd9275067" {
			t.Fatalf("%s params = %#v", method, params)
		}
		switch method {
		case "thread/resume":
			config, _ := payload["config"].(map[string]any)
			if payload["excludeTurns"] != true || payload["model"] != "gpt-5.6-sol" || config["model_reasoning_effort"] != "high" {
				t.Fatalf("resume params = %#v", payload)
			}
			return json.RawMessage(`{"thread":{"id":"019f351f-9a67-76e0-8ffe-34fdd9275067"}}`), nil
		case "thread/queue/start":
			if payload["queuedSubmissionId"] != "submission-1" {
				t.Fatalf("queue start params = %#v", payload)
			}
			return json.RawMessage(`{"turn":{"id":"turn-1"}}`), nil
		default:
			t.Fatalf("unexpected method %q", method)
			return nil, nil
		}
	}

	turnID, err := startCodexQueuedMessage(
		call,
		"019f351f-9a67-76e0-8ffe-34fdd9275067",
		"submission-1",
		true,
		CodexMessageOptions{Model: "gpt-5.6-sol", ReasoningEffort: "high"},
	)
	if err != nil {
		t.Fatalf("startCodexQueuedMessage() error = %v", err)
	}
	if turnID != "turn-1" || strings.Join(methods, ",") != "thread/resume,thread/queue/start" {
		t.Fatalf("turnID = %q, methods = %v", turnID, methods)
	}
}

func TestStartCodexThreadMessagePersistsUserInputBeforeTurn(t *testing.T) {
	var methods []string
	call := func(method string, params any) (json.RawMessage, error) {
		methods = append(methods, method)
		payload := params.(map[string]any)
		if payload["threadId"] != "019f351f-9a67-76e0-8ffe-34fdd9275067" {
			t.Fatalf("%s params = %#v", method, params)
		}
		switch method {
		case "thread/resume":
			if payload["excludeTurns"] != true {
				t.Fatalf("resume params = %#v", payload)
			}
			return json.RawMessage(`{"thread":{"id":"019f351f-9a67-76e0-8ffe-34fdd9275067"}}`), nil
		case "turn/start":
			if payload["clientUserMessageId"] != "client-message-1" || payload["model"] != "gpt-5.6-sol" || payload["effort"] != "high" {
				t.Fatalf("turn params = %#v", payload)
			}
			input, ok := payload["input"].([]map[string]any)
			if !ok || len(input) != 1 || input[0]["type"] != "text" || input[0]["text"] != "创建 writing · Results-1.3.3" {
				t.Fatalf("turn input = %#v", payload["input"])
			}
			return json.RawMessage(`{"turn":{"id":"turn-1"}}`), nil
		default:
			t.Fatalf("unexpected method %q", method)
			return nil, nil
		}
	}

	turnID, err := startCodexThreadMessage(
		call,
		"019f351f-9a67-76e0-8ffe-34fdd9275067",
		"创建 writing · Results-1.3.3",
		"client-message-1",
		true,
		CodexMessageOptions{Model: "gpt-5.6-sol", ReasoningEffort: "high"},
	)
	if err != nil {
		t.Fatalf("startCodexThreadMessage() error = %v", err)
	}
	if turnID != "turn-1" || strings.Join(methods, ",") != "thread/resume,turn/start" {
		t.Fatalf("turnID = %q, methods = %v", turnID, methods)
	}
}

func TestStartCodexQueuedMessageUpdatesLoadedThreadSettings(t *testing.T) {
	var methods []string
	call := func(method string, params any) (json.RawMessage, error) {
		methods = append(methods, method)
		payload := params.(map[string]any)
		switch method {
		case "thread/settings/update":
			if payload["model"] != "gpt-5.6-luna" || payload["effort"] != "max" {
				t.Fatalf("settings params = %#v", payload)
			}
			return json.RawMessage(`{}`), nil
		case "thread/queue/start":
			return json.RawMessage(`{"turn":{"id":"turn-2"}}`), nil
		default:
			t.Fatalf("unexpected method %q", method)
			return nil, nil
		}
	}

	turnID, err := startCodexQueuedMessage(
		call,
		"019f351f-9a67-76e0-8ffe-34fdd9275067",
		"submission-2",
		false,
		CodexMessageOptions{Model: "gpt-5.6-luna", ReasoningEffort: "max"},
	)
	if err != nil || turnID != "turn-2" || strings.Join(methods, ",") != "thread/settings/update,thread/queue/start" {
		t.Fatalf("turnID = %q, methods = %v, error = %v", turnID, methods, err)
	}
}

func TestCodexActiveWriterErrorLeavesQueuedMessageForCurrentWriter(t *testing.T) {
	err := fmt.Errorf("resume Codex thread: Codex thread/resume failed (-32600): thread 019f351f-9a67-76e0-8ffe-34fdd9275067 already has an active writer")
	if !isCodexActiveWriterError(err) {
		t.Fatalf("isCodexActiveWriterError(%q) = false", err)
	}
	if isCodexActiveWriterError(errors.New("thread already has an active or pending turn")) {
		t.Fatal("ordinary active turn must not be treated as an active writer conflict")
	}

	_, startErr := startCodexQueuedMessage(func(method string, _ any) (json.RawMessage, error) {
		if method != "thread/resume" {
			t.Fatalf("unexpected method %q", method)
		}
		return nil, err
	}, "019f351f-9a67-76e0-8ffe-34fdd9275067", "submission-1", true, CodexMessageOptions{})
	if !isCodexActiveWriterError(startErr) {
		t.Fatalf("start error = %v, want active-writer error", startErr)
	}
}

func TestCodexSendMessageHandlerStartsTurn(t *testing.T) {
	var gotThreadID, gotMessage string
	var gotOptions CodexMessageOptions
	handler := codexSendMessageHandler(func(_ context.Context, threadID, message string, options CodexMessageOptions) (CodexMessageResult, error) {
		gotThreadID, gotMessage = threadID, message
		gotOptions = options
		return CodexMessageResult{MessageID: "client-message-1", TurnID: "turn-1", State: "started"}, nil
	})
	req := httptest.NewRequest(http.MethodPost,
		"/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27&model=gpt-5.6-sol&reasoning_effort=high",
		strings.NewReader("  继续检查  "))
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotThreadID != "019ed804-66d5-7361-b92a-996db5525f27" || gotMessage != "继续检查" {
		t.Fatalf("send args = %q, %q", gotThreadID, gotMessage)
	}
	if gotOptions.Model != "gpt-5.6-sol" || gotOptions.ReasoningEffort != "high" {
		t.Fatalf("send options = %+v", gotOptions)
	}
	var response struct {
		MessageID string `json:"message_id"`
		TurnID    string `json:"turn_id"`
		State     string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.MessageID != "client-message-1" || response.TurnID != "turn-1" || response.State != "started" {
		t.Fatalf("response = %q, err = %v", rec.Body.String(), err)
	}
}

func TestCodexSendMessageHandlerReportsQueuedMessage(t *testing.T) {
	handler := codexSendMessageHandler(func(context.Context, string, string, CodexMessageOptions) (CodexMessageResult, error) {
		return CodexMessageResult{MessageID: "client-message-2", State: "queued"}, nil
	})
	req := httptest.NewRequest(http.MethodPost,
		"/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27",
		strings.NewReader("稍后继续"))
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response CodexMessageResult
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.MessageID != "client-message-2" || response.TurnID != "" || response.State != "queued" {
		t.Fatalf("response = %q, err = %v", rec.Body.String(), err)
	}
}

func TestCodexSendMessageHandlerRejectsInvalidInput(t *testing.T) {
	handler := codexSendMessageHandler(func(context.Context, string, string, CodexMessageOptions) (CodexMessageResult, error) {
		t.Fatal("sender must not run")
		return CodexMessageResult{}, nil
	})
	tests := []struct {
		name   string
		path   string
		body   string
		origin string
		want   int
	}{
		{name: "invalid thread", path: "/codex/send-message?thread_id=bad", body: "继续", origin: "http://localhost:3000", want: http.StatusBadRequest},
		{name: "empty message", path: "/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27", body: "  ", origin: "http://localhost:3000", want: http.StatusBadRequest},
		{name: "foreign origin", path: "/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27", body: "继续", origin: "https://example.com", want: http.StatusForbidden},
		{name: "invalid model", path: "/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27", body: `{"message":"继续","model":"bad model"}`, origin: "http://localhost:3000", want: http.StatusBadRequest},
		{name: "wrong method", path: "/codex/send-message?thread_id=019ed804-66d5-7361-b92a-996db5525f27", body: "继续", origin: "http://localhost:3000", want: http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodPost
			if tc.name == "wrong method" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Origin", tc.origin)
			if strings.HasPrefix(tc.body, "{") {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCodexThreadsHandlerRejectsWritesAndForeignOrigins(t *testing.T) {
	handler := codexThreadsHandler(func(context.Context) (CodexCatalog, error) {
		t.Fatal("lister must not run")
		return CodexCatalog{}, nil
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
