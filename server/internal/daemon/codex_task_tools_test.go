package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const wallTaskTestID = "01a08145-50f2-7350-af67-63cfa4637feb"

func TestCreateWallTaskNamesAndReadsBack(t *testing.T) {
	var methods []string
	call := func(method string, params any) (json.RawMessage, error) {
		methods = append(methods, method)
		p := params.(map[string]any)
		switch method {
		case "thread/start":
			if p["historyMode"] != "legacy" {
				t.Fatal("empty tasks must survive provider restart")
			}
			if p["cwd"] != "/project" || p["projectId"] != "project-id" {
				t.Fatalf("wrong project: %v", p)
			}
			return json.RawMessage(`{"thread":{"id":"` + wallTaskTestID + `"}}`), nil
		case "thread/name/set":
			if p["name"] != "method · 05" || p["threadId"] != wallTaskTestID {
				t.Fatalf("wrong name: %v", p)
			}
			return json.RawMessage(`{}`), nil
		case "thread/read":
			return json.RawMessage(`{"thread":{"id":"` + wallTaskTestID + `","name":"method · 05","cwd":"/project","status":{"type":"idle"}}}`), nil
		default:
			t.Fatalf("unexpected method %s", method)
			return nil, nil
		}
	}
	thread, err := createWallTask(call, wallToolThread{CWD: "/project", ProjectID: "project-id"}, "method · 05")
	if err != nil || thread.Name != "method · 05" || thread.Status.Type != "idle" {
		t.Fatalf("result: %+v, %v", thread, err)
	}
	if strings.Join(methods, ",") != "thread/start,thread/name/set,thread/read" {
		t.Fatal(methods)
	}
}

func TestCreateWallTaskRejectsInvalidNameBeforeCreation(t *testing.T) {
	for _, title := range []string{"", "method", " · 05", "method · ", " method · 05"} {
		_, err := createWallTask(func(string, any) (json.RawMessage, error) { t.Fatal("invalid input reached provider"); return nil, nil }, wallToolThread{}, title)
		if err == nil {
			t.Fatalf("accepted %q", title)
		}
	}
}

func TestCreateWallTaskFailurePreservesCreatedID(t *testing.T) {
	_, err := createWallTask(func(method string, _ any) (json.RawMessage, error) {
		if method == "thread/start" {
			return json.RawMessage(`{"thread":{"id":"` + wallTaskTestID + `"}}`), nil
		}
		return nil, errors.New("name failed")
	}, wallToolThread{CWD: "/project"}, "method · 05")
	if err == nil || !strings.Contains(err.Error(), wallTaskTestID) || !strings.Contains(err.Error(), "do not retry") {
		t.Fatal(err)
	}
}

func TestWallRouterConfigDoesNotChangeOrdinaryChat(t *testing.T) {
	for _, name := range []string{"implementation · 05", "writing · Results", "router · 创建对话", "AI+ASD Router"} {
		config := wallRouterToolConfig(func(string, any) (json.RawMessage, error) {
			return json.RawMessage(`{"thread":{"id":"` + wallTaskTestID + `","name":"` + name + `","cwd":"/project"}}`), nil
		}, "/codex", "/wall", wallTaskTestID)
		if (config != nil) != isWallRouter(name) {
			t.Fatalf("wrong config for %s", name)
		}
	}
}

func TestWallToolsDiscoveryDoesNotLaunchCodex(t *testing.T) {
	var out bytes.Buffer
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`
	if err := ServeCodexTaskTools("/no-real-codex", wallTaskTestID, strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"create_thread"`) || !strings.Contains(lines[1], `"read_thread"`) {
		t.Fatal(out.String())
	}
}

func TestRouterToolsPreserveMessageModelAndEffort(t *testing.T) {
	config := map[string]any{"mcp_servers.session_wall": map[string]any{"command": "/wall"}}
	var called []string
	_, err := startCodexThreadMessage(func(method string, params any) (json.RawMessage, error) {
		called = append(called, method)
		p := params.(map[string]any)
		if method == "thread/resume" {
			if p["config"] == nil || p["threadId"] != wallTaskTestID {
				t.Fatal(p)
			}
			return json.RawMessage(`{}`), nil
		}
		if method != "turn/start" || p["model"] != "selected-model" || p["effort"] != "high" || p["clientUserMessageId"] != "message-id" {
			t.Fatal(p)
		}
		if p["input"].([]map[string]any)[0]["text"] != "original message" {
			t.Fatal("message changed")
		}
		return json.RawMessage(`{"turn":{"id":"turn-id"}}`), nil
	}, wallTaskTestID, "original message", "message-id", true, CodexMessageOptions{Model: "selected-model", ReasoningEffort: "high", RouterToolConfig: config})
	if err != nil || strings.Join(called, ",") != "thread/resume,turn/start" {
		t.Fatalf("%v %v", called, err)
	}
}
