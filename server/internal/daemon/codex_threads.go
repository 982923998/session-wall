package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	codexThreadListTimeout = 60 * time.Second
	codexPreviewLimit      = 320
	codexTranscriptLimit   = 20
	codexTranscriptItemMax = 120
	codexTranscriptTextMax = 8_000
	codexMessageTextMax    = 16 * 1024
)

// CodexThreadSummary is the read-only catalog entry consumed by the local card wall.
type CodexThreadSummary struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	Preview          string `json:"preview,omitempty"`
	CWD              string `json:"cwd,omitempty"`
	ProjectRoot      string `json:"project_root,omitempty"`
	Status           string `json:"status,omitempty"`
	Branch           string `json:"branch,omitempty"`
	Model            string `json:"model,omitempty"`
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
	ParentThreadID   string `json:"parent_thread_id,omitempty"`
	Delegated        bool   `json:"delegated,omitempty"`
	LatestTurnOrigin string `json:"latest_turn_origin,omitempty"`
	CreatedAt        int64  `json:"created_at,omitempty"`
	UpdatedAt        int64  `json:"updated_at,omitempty"`
	RecencyAt        int64  `json:"recency_at,omitempty"`
	Pinned           bool   `json:"pinned"`
	AgentRole        string `json:"agent_role,omitempty"`
	AgentName        string `json:"agent_nickname,omitempty"`
}

// CodexProjectSummary is the project name and root catalog exposed by Codex.
type CodexProjectSummary struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Position int64    `json:"position"`
	Roots    []string `json:"roots"`
}

type CodexCatalog struct {
	Threads         []CodexThreadSummary  `json:"threads"`
	ArchivedThreads []CodexThreadSummary  `json:"archived_threads,omitempty"`
	Projects        []CodexProjectSummary `json:"projects"`
}

type CodexReasoningEffort struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type CodexModelSummary struct {
	ID                     string                 `json:"id"`
	Name                   string                 `json:"name"`
	Description            string                 `json:"description,omitempty"`
	DefaultReasoningEffort string                 `json:"default_reasoning_effort"`
	ReasoningEfforts       []CodexReasoningEffort `json:"reasoning_efforts"`
	Default                bool                   `json:"default"`
}

type CodexMessageOptions struct {
	RouterToolConfig map[string]any `json:"-"`
	Interrupt        bool           `json:"interrupt,omitempty"`
	Model            string         `json:"model"`
	ReasoningEffort  string         `json:"reasoning_effort"`
}

type CodexMessageResult struct {
	NativeMessageID string `json:"native_message_id,omitempty"`
	Error           string `json:"error,omitempty"`
	MessageID       string `json:"message_id"`
	TurnID          string `json:"turn_id,omitempty"`
	State           string `json:"state"`
}

type CodexThreadRestoreResult struct {
	ThreadID string `json:"thread_id"`
	CWD      string `json:"cwd,omitempty"`
}

// CodexTranscriptItem is the small, display-oriented subset of a persisted
// Codex turn item used by the local Session Wall conversation preview.
type CodexTranscriptItem struct {
	ClientID string `json:"client_id,omitempty"`
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Text     string `json:"text,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status,omitempty"`
	Phase    string `json:"phase,omitempty"`
}

type CodexThreadTranscript struct {
	TurnID   string                `json:"turn_id,omitempty"`
	ThreadID string                `json:"thread_id"`
	Status   string                `json:"status,omitempty"`
	Items    []CodexTranscriptItem `json:"items"`
}

type codexProjectSelection struct {
	Projects []CodexProjectSummary `json:"projects"`
}

type codexThreadListResult struct {
	Data []struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Title     string          `json:"title"`
		Preview   string          `json:"preview"`
		CWD       string          `json:"cwd"`
		CreatedAt int64           `json:"createdAt"`
		UpdatedAt int64           `json:"updatedAt"`
		RecencyAt int64           `json:"recencyAt"`
		AgentRole string          `json:"agentRole"`
		AgentName string          `json:"agentNickname"`
		Source    json.RawMessage `json:"source"`
		Status    struct {
			Type string `json:"type"`
		} `json:"status"`
		GitInfo struct {
			Branch string `json:"branch"`
		} `json:"gitInfo"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type codexModelListResult struct {
	Data []struct {
		ID                        string `json:"id"`
		Model                     string `json:"model"`
		DisplayName               string `json:"displayName"`
		Description               string `json:"description"`
		Hidden                    bool   `json:"hidden"`
		DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
		SupportedReasoningEfforts []struct {
			ReasoningEffort string `json:"reasoningEffort"`
			Description     string `json:"description"`
		} `json:"supportedReasoningEfforts"`
		Default bool `json:"isDefault"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type codexModelsCache struct {
	Models []struct {
		Slug                    string `json:"slug"`
		DisplayName             string `json:"display_name"`
		Description             string `json:"description"`
		Visibility              string `json:"visibility"`
		DefaultReasoningEffort  string `json:"default_reasoning_level"`
		SupportedReasoningLevel []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func delegationRole(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "neuro-ai-research-router"), strings.Contains(lower, "router role agent"), strings.Contains(lower, "router agent"):
		return "router"
	case strings.Contains(lower, "neuro-ai-analysis-implementation"), strings.Contains(lower, "implementation role agent"):
		return "implementation"
	case strings.Contains(lower, "neuro-ai-method-details"), strings.Contains(lower, "method role agent"):
		return "method"
	case strings.Contains(lower, "neuro-ai-visualization"), strings.Contains(lower, "visualization role agent"):
		return "visualization"
	case strings.Contains(lower, "neuro-ai-server-experiment"), strings.Contains(lower, "server role agent"):
		return "server-experiment"
	case strings.Contains(lower, "neuro-ai-writing"), strings.Contains(lower, "writing role agent"):
		return "writing"
	case strings.Contains(lower, "neuro-ai-rebuttal"), strings.Contains(lower, "reviewer role agent"):
		return "reviewer"
	default:
		return ""
	}
}

func codexThreadSourceIsDelegated(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	var source any
	if json.Unmarshal([]byte(value), &source) == nil {
		switch typed := source.(type) {
		case string:
			return strings.EqualFold(strings.TrimSpace(typed), "subagent")
		case map[string]any:
			_, delegated := typed["subagent"]
			return delegated
		}
	}
	return strings.EqualFold(value, "subagent")
}

func codexThreadRole(agentRole, title, preview string) string {
	if role := strings.TrimSpace(agentRole); role != "" {
		return role
	}
	for _, candidate := range []string{title, preview} {
		if role := delegationRole(candidate); role != "" {
			return role
		}
	}
	return ""
}

func delegationScope(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"objective / scope:", "功能标记：", "scope:"} {
		index := strings.Index(lower, strings.ToLower(marker))
		if index < 0 {
			continue
		}
		rest := strings.TrimSpace(value[index+len(marker):])
		for offset, char := range rest {
			if char == '\n' || char == '\r' || char == '；' || char == ';' || char == '，' || char == ',' {
				rest = rest[:offset]
				break
			}
		}
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "branch:"))
		if rest != "" {
			return rest
		}
	}
	return ""
}

func inferDelegationName(value string) string {
	if !strings.Contains(strings.ToLower(value), "<codex_delegation>") {
		return ""
	}
	role := delegationRole(value)
	scope := delegationScope(value)
	if role == "" || scope == "" {
		return ""
	}
	return role + " · " + scope
}

func codexThreadDisplayName(name, title, preview string) string {
	if value := strings.TrimSpace(name); value != "" {
		return truncateRunes(value, 160)
	}
	for _, candidate := range []string{title, preview} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if inferred := inferDelegationName(candidate); inferred != "" {
			return inferred
		}
		if !strings.HasPrefix(strings.ToLower(candidate), "<codex_delegation>") {
			return truncateRunes(candidate, 160)
		}
	}
	return ""
}

func decodeCodexThreadListResult(raw json.RawMessage) ([]CodexThreadSummary, *string, error) {
	var result codexThreadListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, nil, fmt.Errorf("decode Codex thread list: %w", err)
	}
	threads := make([]CodexThreadSummary, 0, len(result.Data))
	for _, thread := range result.Data {
		if strings.TrimSpace(thread.ID) == "" {
			continue
		}
		threads = append(threads, CodexThreadSummary{
			ID:        thread.ID,
			Name:      codexThreadDisplayName(thread.Name, thread.Title, thread.Preview),
			Preview:   truncateRunes(thread.Preview, codexPreviewLimit),
			CWD:       thread.CWD,
			Status:    thread.Status.Type,
			Branch:    thread.GitInfo.Branch,
			CreatedAt: thread.CreatedAt,
			UpdatedAt: thread.UpdatedAt,
			RecencyAt: thread.RecencyAt,
			AgentRole: codexThreadRole(thread.AgentRole, thread.Title, thread.Preview),
			AgentName: thread.AgentName,
			Delegated: codexThreadSourceIsDelegated(string(thread.Source)),
		})
	}
	return threads, result.NextCursor, nil
}

func decodeCodexModelListResult(raw json.RawMessage) ([]CodexModelSummary, *string, error) {
	var result codexModelListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, nil, fmt.Errorf("decode Codex model list: %w", err)
	}
	models := make([]CodexModelSummary, 0, len(result.Data))
	for _, model := range result.Data {
		id := strings.TrimSpace(model.Model)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if id == "" || model.Hidden {
			continue
		}
		efforts := make([]CodexReasoningEffort, 0, len(model.SupportedReasoningEfforts))
		for _, effort := range model.SupportedReasoningEfforts {
			if value := strings.TrimSpace(effort.ReasoningEffort); value != "" {
				efforts = append(efforts, CodexReasoningEffort{
					Value: value, Description: strings.TrimSpace(effort.Description),
				})
			}
		}
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = id
		}
		models = append(models, CodexModelSummary{
			ID:                     id,
			Name:                   name,
			Description:            strings.TrimSpace(model.Description),
			DefaultReasoningEffort: strings.TrimSpace(model.DefaultReasoningEffort),
			ReasoningEfforts:       efforts,
			Default:                model.Default,
		})
	}
	return models, result.NextCursor, nil
}

func readCodexModelsCache(filename string) ([]CodexModelSummary, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read Codex model cache: %w", err)
	}
	var cache codexModelsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("decode Codex model cache: %w", err)
	}
	models := make([]CodexModelSummary, 0, len(cache.Models))
	for _, model := range cache.Models {
		id := strings.TrimSpace(model.Slug)
		if id == "" || strings.TrimSpace(model.Visibility) != "list" {
			continue
		}
		efforts := make([]CodexReasoningEffort, 0, len(model.SupportedReasoningLevel))
		for _, effort := range model.SupportedReasoningLevel {
			if value := strings.TrimSpace(effort.Effort); value != "" {
				efforts = append(efforts, CodexReasoningEffort{
					Value: value, Description: strings.TrimSpace(effort.Description),
				})
			}
		}
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = id
		}
		defaultEffort := strings.TrimSpace(model.DefaultReasoningEffort)
		if defaultEffort == "" && len(efforts) > 0 {
			defaultEffort = efforts[0].Value
		}
		models = append(models, CodexModelSummary{
			ID:                     id,
			Name:                   name,
			Description:            strings.TrimSpace(model.Description),
			DefaultReasoningEffort: defaultEffort,
			ReasoningEfforts:       efforts,
			Default:                len(models) == 0,
		})
	}
	if len(models) == 0 {
		return nil, errors.New("Codex model cache contains no visible models")
	}
	return models, nil
}

type codexProjectListResult struct {
	Data []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Position int64  `json:"position"`
		Roots    []struct {
			Path string `json:"path"`
		} `json:"roots"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

func decodeCodexProjectListResult(raw json.RawMessage) ([]CodexProjectSummary, *string, error) {
	var result codexProjectListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, nil, fmt.Errorf("decode Codex project list: %w", err)
	}
	projects := make([]CodexProjectSummary, 0, len(result.Data))
	for _, project := range result.Data {
		if strings.TrimSpace(project.ID) == "" || strings.TrimSpace(project.Name) == "" {
			continue
		}
		roots := make([]string, 0, len(project.Roots))
		for _, root := range project.Roots {
			if path := strings.TrimSpace(root.Path); path != "" {
				roots = append(roots, path)
			}
		}
		projects = append(projects, CodexProjectSummary{
			ID: project.ID, Name: project.Name, Position: project.Position, Roots: roots,
		})
	}
	return projects, result.NextCursor, nil
}

type codexCall func(string, any) (json.RawMessage, error)

func isCodexActiveWriterError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already has an active writer")
}

func unarchiveCodexThread(call codexCall, threadID string) (CodexThreadRestoreResult, error) {
	raw, err := call("thread/unarchive", map[string]any{"threadId": threadID})
	if err != nil {
		return CodexThreadRestoreResult{}, fmt.Errorf("restore Codex thread: %w", err)
	}
	var response struct {
		Thread struct {
			ID  string `json:"id"`
			CWD string `json:"cwd"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return CodexThreadRestoreResult{}, fmt.Errorf("decode restored Codex thread: %w", err)
	}
	restoredID := strings.TrimSpace(response.Thread.ID)
	if restoredID == "" || restoredID != threadID {
		return CodexThreadRestoreResult{}, errors.New("restored Codex thread id does not match the request")
	}
	return CodexThreadRestoreResult{
		ThreadID: restoredID,
		CWD:      strings.TrimSpace(response.Thread.CWD),
	}, nil
}

func queueCodexThreadMessage(
	call codexCall,
	threadID, message, clientUserMessageID string,
) (string, error) {
	raw, err := call("thread/queue/add", map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type":          "text",
			"text":          message,
			"text_elements": []any{},
		}},
		"clientUserMessageId": clientUserMessageID,
	})
	if err != nil {
		return "", fmt.Errorf("queue Codex message: %w", err)
	}
	var response struct {
		QueuedSubmission struct {
			ID string `json:"id"`
		} `json:"queuedSubmission"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex queued message: %w", err)
	}
	if strings.TrimSpace(response.QueuedSubmission.ID) == "" {
		return "", errors.New("Codex queued message id is missing")
	}
	return response.QueuedSubmission.ID, nil
}

func listCodexQueuedMessageIDs(call codexCall, threadID string) ([]string, error) {
	raw, err := call("thread/queue/list", map[string]any{
		"threadId": threadID,
		"limit":    100,
	})
	if err != nil {
		return nil, fmt.Errorf("list queued Codex messages: %w", err)
	}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode queued Codex messages: %w", err)
	}
	ids := make([]string, 0, len(response.Data))
	for _, submission := range response.Data {
		if id := strings.TrimSpace(submission.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func startCodexQueuedMessage(
	call codexCall,
	threadID, submissionID string,
	resume bool,
	options CodexMessageOptions,
) (string, error) {
	if resume {
		params := map[string]any{
			"threadId":     threadID,
			"excludeTurns": true,
		}
		if options.Model != "" {
			params["model"] = options.Model
		}
		if options.ReasoningEffort != "" {
			params["config"] = map[string]any{"model_reasoning_effort": options.ReasoningEffort}
		}
		if _, err := call("thread/resume", params); err != nil {
			return "", fmt.Errorf("resume Codex thread: %w", err)
		}
	} else if options.Model != "" || options.ReasoningEffort != "" {
		params := map[string]any{"threadId": threadID}
		if options.Model != "" {
			params["model"] = options.Model
		}
		if options.ReasoningEffort != "" {
			params["effort"] = options.ReasoningEffort
		}
		if _, err := call("thread/settings/update", params); err != nil {
			return "", fmt.Errorf("update Codex thread settings: %w", err)
		}
	}
	raw, err := call("thread/queue/start", map[string]any{
		"threadId":           threadID,
		"queuedSubmissionId": submissionID,
	})
	if err != nil {
		return "", fmt.Errorf("start queued Codex message: %w", err)
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex turn: %w", err)
	}
	if strings.TrimSpace(response.Turn.ID) == "" {
		return "", errors.New("Codex turn id is missing")
	}
	return response.Turn.ID, nil
}

func startCodexThreadMessage(
	call codexCall,
	threadID, message, clientUserMessageID string,
	resume bool,
	options CodexMessageOptions,
) (string, error) {
	if resume {
		params := map[string]any{
			"threadId":     threadID,
			"excludeTurns": true,
		}
		if options.RouterToolConfig != nil {
			params["config"] = options.RouterToolConfig
		}
		if _, err := call("thread/resume", params); err != nil {
			return "", fmt.Errorf("resume Codex thread: %w", err)
		}
	}
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type":          "text",
			"text":          message,
			"text_elements": []any{},
		}},
		"clientUserMessageId": clientUserMessageID,
	}
	if options.Model != "" {
		params["model"] = options.Model
	}
	if options.ReasoningEffort != "" {
		params["effort"] = options.ReasoningEffort
	}
	raw, err := call("turn/start", params)
	if err != nil {
		return "", fmt.Errorf("start Codex turn: %w", err)
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex turn: %w", err)
	}
	if strings.TrimSpace(response.Turn.ID) == "" {
		return "", errors.New("Codex turn id is missing")
	}
	return response.Turn.ID, nil
}

type codexThreadTurnsListResult struct {
	Data []struct {
		Items  []json.RawMessage `json:"items"`
		Status string            `json:"status"`
	} `json:"data"`
}

func normalizeCodexTranscriptItem(raw json.RawMessage) (CodexTranscriptItem, bool) {
	var item struct {
		ClientID         string `json:"clientId"`
		Type             string `json:"type"`
		ID               string `json:"id"`
		Text             string `json:"text"`
		Phase            string `json:"phase"`
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregatedOutput"`
		Query            string `json:"query"`
		Status           string `json:"status"`
		Content          []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Summary []string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return CodexTranscriptItem{}, false
	}

	switch item.Type {
	case "userMessage":
		parts := make([]string, 0, len(item.Content))
		for _, content := range item.Content {
			if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		return CodexTranscriptItem{ID: item.ID, ClientID: item.ClientID, Kind: "user", Text: text}, text != ""
	case "agentMessage":
		text := strings.TrimSpace(item.Text)
		return CodexTranscriptItem{ID: item.ID, Kind: "assistant", Text: text, Phase: item.Phase}, text != ""
	case "commandExecution":
		return CodexTranscriptItem{
			ID: item.ID, Kind: "tool", Title: truncateRunes(item.Command, 600),
			Text: truncateRunes(item.AggregatedOutput, codexTranscriptTextMax), Status: item.Status,
		}, strings.TrimSpace(item.Command) != "" || strings.TrimSpace(item.AggregatedOutput) != ""
	case "webSearch":
		query := strings.TrimSpace(item.Query)
		return CodexTranscriptItem{ID: item.ID, Kind: "tool", Title: "网页搜索：" + query, Status: item.Status}, query != ""
	case "reasoning":
		text := strings.TrimSpace(strings.Join(item.Summary, "\n"))
		return CodexTranscriptItem{ID: item.ID, Kind: "activity", Title: "思考", Text: text}, text != ""
	default:
		return CodexTranscriptItem{}, false
	}
}

func trimCodexTranscriptItems(items []CodexTranscriptItem) []CodexTranscriptItem {
	if len(items) <= codexTranscriptItemMax {
		return items
	}
	keep := make([]bool, len(items))
	kept := 0
	for index, item := range items {
		if item.Kind == "user" {
			keep[index] = true
			kept++
		}
	}
	for index := len(items) - 1; index >= 0 && kept < codexTranscriptItemMax; index-- {
		if !keep[index] {
			keep[index] = true
			kept++
		}
	}
	trimmed := make([]CodexTranscriptItem, 0, kept)
	for index, item := range items {
		if keep[index] {
			trimmed = append(trimmed, item)
		}
	}
	return trimmed
}

func readCodexThreadTranscript(call codexCall, threadID string) (CodexThreadTranscript, error) {
	raw, err := call("thread/turns/list", map[string]any{
		"threadId": threadID, "limit": codexTranscriptLimit,
		"sortDirection": "desc", "itemsView": "full",
	})
	if err != nil {
		return CodexThreadTranscript{}, err
	}
	var result codexThreadTurnsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return CodexThreadTranscript{}, fmt.Errorf("decode Codex thread transcript: %w", err)
	}
	transcript := CodexThreadTranscript{ThreadID: threadID, Items: []CodexTranscriptItem{}}
	if len(result.Data) > 0 {
		transcript.Status = codexCardThreadStatus(result.Data[0].Status)
	}
	for turnIndex := len(result.Data) - 1; turnIndex >= 0; turnIndex-- {
		for _, rawItem := range result.Data[turnIndex].Items {
			if item, ok := normalizeCodexTranscriptItem(rawItem); ok {
				transcript.Items = append(transcript.Items, item)
			}
		}
	}
	transcript.Items = trimCodexTranscriptItems(transcript.Items)
	return transcript, nil
}

func listCodexThreads(call codexCall, cwdFilter []string) ([]CodexThreadSummary, error) {
	var threads []CodexThreadSummary
	seen := make(map[string]bool)
	var cursor *string
	for {
		params := map[string]any{
			"cursor": cursor, "limit": 100, "sortKey": "recency_at", "sortDirection": "desc",
			"archived": false, "modelProviders": []string{},
			"sourceKinds": []string{"cli", "vscode", "appServer", "exec"},
		}
		if len(cwdFilter) > 0 {
			params["cwd"] = cwdFilter
		}
		raw, err := call("thread/list", params)
		if err != nil {
			return nil, err
		}
		page, next, err := decodeCodexThreadListResult(raw)
		if err != nil {
			return nil, err
		}
		for _, thread := range page {
			if !seen[thread.ID] {
				seen[thread.ID] = true
				threads = append(threads, thread)
			}
		}
		if next == nil || strings.TrimSpace(*next) == "" || (cursor != nil && *next == *cursor) {
			return threads, nil
		}
		cursor = next
	}
}

func listAllCodexThreads(call codexCall) ([]CodexThreadSummary, error) {
	return listCodexThreads(call, nil)
}

func listAllCodexModels(call codexCall) ([]CodexModelSummary, error) {
	models := make([]CodexModelSummary, 0)
	var cursor *string
	for {
		raw, err := call("model/list", map[string]any{
			"cursor": cursor, "limit": 100, "includeHidden": false,
		})
		if err != nil {
			return nil, err
		}
		page, next, err := decodeCodexModelListResult(raw)
		if err != nil {
			return nil, err
		}
		models = append(models, page...)
		if next == nil || strings.TrimSpace(*next) == "" || (cursor != nil && *next == *cursor) {
			return models, nil
		}
		cursor = next
	}
}

func readCodexProjectSelection(filename string) ([]CodexProjectSummary, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read Codex project selection: %w", err)
	}
	var selection codexProjectSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return nil, fmt.Errorf("decode Codex project selection: %w", err)
	}
	seen := make(map[string]bool)
	projects := make([]CodexProjectSummary, 0, len(selection.Projects))
	for _, project := range selection.Projects {
		project.ID = strings.TrimSpace(project.ID)
		project.Name = strings.TrimSpace(project.Name)
		if project.ID == "" || project.Name == "" || seen[project.ID] {
			continue
		}
		roots := make([]string, 0, len(project.Roots))
		for _, root := range project.Roots {
			if root = strings.TrimSpace(root); root != "" {
				roots = append(roots, root)
			}
		}
		if len(roots) == 0 {
			continue
		}
		project.Roots = roots
		seen[project.ID] = true
		projects = append(projects, project)
	}
	if len(projects) == 0 {
		return nil, errors.New("Codex project selection contains no usable local projects")
	}
	return projects, nil
}

func projectRootForThread(project CodexProjectSummary, cwd string) string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	best := ""
	for _, candidate := range project.Roots {
		root := filepath.Clean(candidate)
		if cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator)) {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best
}

func projectRootFromWorktreeMetadata(cwd string) string {
	data, err := os.ReadFile(filepath.Join(cwd, ".git"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if gitDir == "" {
		return ""
	}
	return projectRootFromGitCommonDir(filepath.Join(gitDir, "..", ".."))
}

func selectedProjectRoot(ctx context.Context, cwd string, projects []CodexProjectSummary) string {
	for _, project := range projects {
		if root := projectRootForThread(project, cwd); root != "" {
			cleanCWD := filepath.Clean(cwd)
			if cleanCWD == root || strings.HasPrefix(cleanCWD, root+string(filepath.Separator)) {
				return root
			}
		}
	}
	if !strings.Contains(filepath.ToSlash(cwd), "/.codex/worktrees/") {
		return ""
	}
	worktreeRoot := projectRootFromWorktreeMetadata(cwd)
	if worktreeRoot == "" {
		gitContext, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		output, err := exec.CommandContext(gitContext, "git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
		if err != nil {
			return ""
		}
		worktreeRoot = projectRootFromGitCommonDir(string(output))
	}
	for _, project := range projects {
		for _, root := range project.Roots {
			if filepath.Clean(root) == worktreeRoot {
				return root
			}
		}
	}
	return ""
}

func selectedProjectCWDs(ctx context.Context, codexHome string, projects []CodexProjectSummary) (map[string]string, error) {
	database := filepath.Join(strings.TrimSpace(codexHome), "state_5.sqlite")
	if strings.TrimSpace(codexHome) == "" {
		return nil, errors.New("Codex home is unavailable for project filtering")
	}
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", database,
		"SELECT DISTINCT cwd FROM threads WHERE archived = 0 AND cwd <> '';").Output()
	if err != nil {
		return nil, fmt.Errorf("read Codex project paths: %w", err)
	}
	selected := make(map[string]string)
	for _, cwd := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		cwd = strings.TrimSpace(cwd)
		if cwd == "" {
			continue
		}
		if root := selectedProjectRoot(ctx, cwd, projects); root != "" {
			selected[cwd] = root
		}
	}
	return selected, nil
}

func listSelectedCodexThreads(call codexCall, cwdRoots map[string]string) ([]CodexThreadSummary, error) {
	if len(cwdRoots) == 0 {
		return []CodexThreadSummary{}, nil
	}
	cwds := make([]string, 0, len(cwdRoots))
	for cwd := range cwdRoots {
		cwds = append(cwds, cwd)
	}
	threads, err := listCodexThreads(call, cwds)
	if err != nil {
		return nil, err
	}
	for index := range threads {
		threads[index].ProjectRoot = cwdRoots[threads[index].CWD]
	}
	return threads, nil
}

func listAllCodexProjects(call codexCall) ([]CodexProjectSummary, error) {
	projects := make([]CodexProjectSummary, 0)
	var cursor *string
	for {
		raw, err := call("project/list", map[string]any{"cursor": cursor, "limit": 100})
		if err != nil {
			return nil, err
		}
		page, next, err := decodeCodexProjectListResult(raw)
		if err != nil {
			return nil, err
		}
		projects = append(projects, page...)
		if next == nil || strings.TrimSpace(*next) == "" || (cursor != nil && *next == *cursor) {
			return projects, nil
		}
		cursor = next
	}
}

func markPinnedThreads(ctx context.Context, codexHome string, threads []CodexThreadSummary) {
	database := filepath.Join(strings.TrimSpace(codexHome), "state_5.sqlite")
	if codexHome == "" {
		return
	}
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", database,
		"SELECT id FROM threads WHERE archived = 0 AND is_pinned = 1;").Output()
	if err != nil {
		return
	}
	pinned := make(map[string]bool)
	for _, id := range strings.Fields(string(output)) {
		pinned[id] = true
	}
	for index := range threads {
		threads[index].Pinned = pinned[threads[index].ID]
	}
}

func markCodexThreadParents(ctx context.Context, codexHome string, threads []CodexThreadSummary) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" || len(threads) == 0 {
		return
	}
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json",
		filepath.Join(codexHome, "state_5.sqlite"),
		"SELECT parent_thread_id, child_thread_id FROM thread_spawn_edges;").Output()
	if err != nil {
		return
	}
	var edges []codexSQLiteSpawnEdge
	if json.Unmarshal(output, &edges) != nil {
		return
	}
	threadIndex := make(map[string]int, len(threads))
	for index := range threads {
		threadIndex[threads[index].ID] = index
	}
	for _, edge := range edges {
		index, ok := threadIndex[strings.TrimSpace(edge.ChildThreadID)]
		if !ok {
			continue
		}
		threads[index].ParentThreadID = strings.TrimSpace(edge.ParentThreadID)
		threads[index].Delegated = true
	}
}

func codexTurnOriginFromUserItem(raw json.RawMessage) string {
	item, ok := normalizeCodexTranscriptItem(raw)
	if !ok || item.Kind != "user" {
		return ""
	}
	message := strings.TrimSpace(item.Text)
	if !strings.HasPrefix(message, "<codex_delegation>") {
		return "direct"
	}
	const openTag = "<source_thread_id>"
	const closeTag = "</source_thread_id>"
	start := strings.Index(message, openTag)
	if start < 0 {
		return "direct"
	}
	start += len(openTag)
	end := strings.Index(message[start:], closeTag)
	if end < 0 || strings.TrimSpace(message[start:start+end]) == "" {
		return "direct"
	}
	return "delegated"
}

func markCodexLatestTurnOrigins(ctx context.Context, codexHome string, threads []CodexThreadSummary) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" || len(threads) == 0 {
		return
	}
	const query = `WITH ranked AS (
  SELECT thread_id, turn_id, first_user_item_id,
    ROW_NUMBER() OVER (
      PARTITION BY thread_id
      ORDER BY COALESCE(started_at, 0) DESC, rollout_ordinal DESC
    ) AS position
  FROM thread_turns
)
SELECT t.thread_id, COALESCE(i.item_json, '') AS item_json
FROM ranked t
LEFT JOIN thread_items i
  ON i.thread_id = t.thread_id
 AND i.turn_id = t.turn_id
 AND i.item_id = t.first_user_item_id
WHERE t.position = 1;`
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json",
		filepath.Join(codexHome, "thread_history_1.sqlite"), query).Output()
	if err != nil {
		return
	}
	var rows []codexSQLiteLatestTurn
	if json.Unmarshal(output, &rows) != nil {
		return
	}
	threadIndex := make(map[string]int, len(threads))
	for index := range threads {
		threads[index].LatestTurnOrigin = ""
		threadIndex[threads[index].ID] = index
	}
	for _, row := range rows {
		index, ok := threadIndex[strings.TrimSpace(row.ThreadID)]
		if !ok {
			continue
		}
		threads[index].LatestTurnOrigin = codexTurnOriginFromUserItem(json.RawMessage(row.ItemJSON))
	}
}

func projectRootFromGitCommonDir(commonDir string) string {
	commonDir = filepath.Clean(strings.TrimSpace(commonDir))
	if filepath.Base(commonDir) != ".git" {
		return ""
	}
	return filepath.Dir(commonDir)
}

func markWorktreeProjectRoots(ctx context.Context, threads []CodexThreadSummary) {
	roots := make(map[string]string)
	for index := range threads {
		cwd := strings.TrimSpace(threads[index].CWD)
		if !strings.Contains(filepath.ToSlash(cwd), "/.codex/worktrees/") {
			continue
		}
		root, checked := roots[cwd]
		if !checked {
			output, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
			if err == nil {
				root = projectRootFromGitCommonDir(string(output))
			}
			roots[cwd] = root
		}
		if root != "" && root != cwd {
			threads[index].ProjectRoot = root
		}
	}
}

type codexRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type codexCatalogClient struct {
	stdin          io.Writer
	stdout         *bufio.Scanner
	nextID         int
	interruptReply chan error
}

func newCodexCatalogClient(stdin io.Writer, stdout io.Reader) *codexCatalogClient {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	return &codexCatalogClient{stdin: stdin, stdout: scanner, interruptReply: make(chan error, 1)}
}

func (c *codexCatalogClient) call(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := json.NewEncoder(c.stdin).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("send Codex %s: %w", method, err)
	}
	for c.stdout.Scan() {
		var response codexRPCResponse
		if err := json.Unmarshal(c.stdout.Bytes(), &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("Codex %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
		}
		return response.Result, nil
	}
	if err := c.stdout.Err(); err != nil {
		return nil, fmt.Errorf("read Codex %s: %w", method, err)
	}
	return nil, fmt.Errorf("read Codex %s: app-server closed", method)
}

func (c *codexCatalogClient) notify(method string) error {
	return json.NewEncoder(c.stdin).Encode(map[string]any{"method": method, "params": map[string]any{}})
}

func (c *codexCatalogClient) drainTurn(turnID string) {
	for c.stdout.Scan() {
		var reply struct {
			ID    string `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(c.stdout.Bytes(), &reply) == nil && reply.ID == "wall-interrupt" {
			var err error
			if reply.Error != nil {
				err = errors.New(reply.Error.Message)
			}
			select {
			case c.interruptReply <- err:
			default:
			}
			continue
		}
		var notification struct {
			Method string `json:"method"`
			Params struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
				TurnID    string `json:"turnId"`
				WillRetry bool   `json:"willRetry"`
			} `json:"params"`
		}
		if json.Unmarshal(c.stdout.Bytes(), &notification) != nil {
			continue
		}
		if notification.Method == "turn/completed" && notification.Params.Turn.ID == turnID {
			return
		}
		if notification.Method == "error" && notification.Params.TurnID == turnID && !notification.Params.WillRetry {
			return
		}
	}
}

// codexCatalogSession keeps the local app-server connection warm. Starting
// Codex is the dominant cost of a refresh, while thread/list itself is cheap.
// The session is serialized because the stdio RPC client has one response
// scanner and is shared by all local refresh requests.
type codexCatalogSession struct {
	executable string

	mu            sync.Mutex
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	stdin         io.WriteCloser
	client        *codexCatalogClient
	codexHome     string
	loadedThreads map[string]bool
	activeMu      sync.Mutex
	activeTurn    string
	activeClient  *codexCatalogClient
}

type codexThreadMessageSession struct {
	queue    *codexCatalogSession
	executor *codexCatalogSession

	mu      sync.Mutex
	options map[string]CodexMessageOptions
}

type codexMessageSession struct {
	executable string

	mu      sync.Mutex
	threads map[string]*codexThreadMessageSession
}

type codexSQLiteThread struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Title           string `json:"title"`
	Preview         string `json:"preview"`
	CWD             string `json:"cwd"`
	GitBranch       string `json:"git_branch"`
	GitOriginURL    string `json:"git_origin_url"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	RecencyAt       int64  `json:"recency_at"`
	AgentRole       string `json:"agent_role"`
	AgentNickname   string `json:"agent_nickname"`
	Source          string `json:"source"`
	Pinned          int64  `json:"is_pinned"`
}

type codexSQLiteThreadStatus struct {
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
}

type codexSQLiteSpawnEdge struct {
	ParentThreadID string `json:"parent_thread_id"`
	ChildThreadID  string `json:"child_thread_id"`
}

type codexSQLiteLatestTurn struct {
	ThreadID string `json:"thread_id"`
	ItemJSON string `json:"item_json"`
}

type codexSQLiteTranscriptRow struct {
	TurnID   string `json:"turn_id"`
	ItemJSON string `json:"item_json"`
	Status   string `json:"status"`
}

func defaultCodexHome() string {
	if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func codexCardThreadStatus(value string) string {
	status := strings.TrimSpace(value)
	switch strings.ToLower(status) {
	case "inprogress", "active", "running":
		return "active"
	case "completed":
		return "completed"
	default:
		return status
	}
}

func listCodexThreadStatusesFromSQLite(ctx context.Context, codexHome string) (map[string]string, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return nil, errors.New("Codex home is unavailable")
	}
	database := filepath.Join(codexHome, "thread_history_1.sqlite")
	const query = `WITH ranked AS (
  SELECT thread_id, status,
    ROW_NUMBER() OVER (
      PARTITION BY thread_id
      ORDER BY COALESCE(started_at, 0) DESC, rollout_ordinal DESC
    ) AS position
  FROM thread_turns
)
SELECT thread_id, status FROM ranked WHERE position = 1;`
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", database, query).Output()
	if err != nil {
		return nil, fmt.Errorf("read Codex thread statuses: %w", err)
	}
	var rows []codexSQLiteThreadStatus
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("decode Codex thread statuses: %w", err)
	}
	statuses := make(map[string]string, len(rows))
	for _, row := range rows {
		threadID := strings.TrimSpace(row.ThreadID)
		status := codexCardThreadStatus(row.Status)
		if threadID != "" && status != "" {
			statuses[threadID] = status
		}
	}
	return statuses, nil
}

func listCodexArchivedThreadIDsFromSQLite(ctx context.Context, codexHome string) ([]string, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return nil, errors.New("Codex home is unavailable")
	}
	database := filepath.Join(codexHome, "state_5.sqlite")
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", database,
		"SELECT id FROM threads WHERE archived != 0 ORDER BY id;").Output()
	if err != nil {
		return nil, fmt.Errorf("read archived Codex threads: %w", err)
	}
	ids := make([]string, 0)
	for _, id := range strings.Fields(string(output)) {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func readCodexThreadTranscriptFromSQLite(ctx context.Context, codexHome, threadID string) (CodexThreadTranscript, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return CodexThreadTranscript{}, errors.New("Codex home is unavailable")
	}
	database := filepath.Join(codexHome, "thread_history_1.sqlite")
	quotedThreadID := sqliteStringLiteral(threadID)
	query := fmt.Sprintf(`WITH recent_turns AS (
  SELECT turn_id, rollout_ordinal, status
  FROM thread_turns
  WHERE thread_id = %s
  ORDER BY rollout_ordinal DESC
  LIMIT %d
), latest_status AS (
  SELECT status FROM recent_turns ORDER BY rollout_ordinal DESC LIMIT 1
)
SELECT i.item_json, COALESCE((SELECT status FROM latest_status), '') AS status,
  (SELECT turn_id FROM recent_turns ORDER BY rollout_ordinal DESC LIMIT 1) AS turn_id
FROM thread_items i
JOIN recent_turns t ON t.turn_id = i.turn_id
WHERE i.thread_id = %s
ORDER BY i.rollout_ordinal ASC;`, quotedThreadID, codexTranscriptLimit, quotedThreadID)
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", database, query).Output()
	if err != nil {
		return CodexThreadTranscript{}, fmt.Errorf("read Codex thread transcript: %w", err)
	}
	var rows []codexSQLiteTranscriptRow
	if err := json.Unmarshal(output, &rows); err != nil {
		return CodexThreadTranscript{}, fmt.Errorf("decode Codex thread transcript: %w", err)
	}
	transcript := CodexThreadTranscript{ThreadID: threadID, Items: []CodexTranscriptItem{}}
	for _, row := range rows {
		if transcript.Status == "" {
			transcript.TurnID = row.TurnID
			transcript.Status = codexCardThreadStatus(row.Status)
		}
		if item, ok := normalizeCodexTranscriptItem(json.RawMessage(row.ItemJSON)); ok {
			transcript.Items = append(transcript.Items, item)
		}
	}
	transcript.Items = trimCodexTranscriptItems(transcript.Items)
	return transcript, nil
}

func projectRootsByGitOrigin(
	ctx context.Context,
	projects []CodexProjectSummary,
	known map[string]string,
) map[string]string {
	roots := make(map[string]string, len(known)+len(projects))
	for origin, root := range known {
		roots[strings.TrimSpace(origin)] = root
	}
	for _, project := range projects {
		for _, root := range project.Roots {
			commandCtx, cancel := context.WithTimeout(ctx, time.Second)
			output, err := exec.CommandContext(commandCtx, "git", "-C", root, "config", "--get", "remote.origin.url").Output()
			cancel()
			if err != nil {
				continue
			}
			if origin := strings.TrimSpace(string(output)); origin != "" {
				if _, exists := roots[origin]; !exists {
					roots[origin] = root
				}
			}
		}
	}
	return roots
}

func listCodexArchivedProjectThreadsFromSQLite(
	ctx context.Context,
	codexHome string,
	projects []CodexProjectSummary,
	knownOriginRoots map[string]string,
) ([]CodexThreadSummary, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" || len(projects) == 0 {
		return nil, errors.New("Codex archived catalog is unavailable")
	}
	database := filepath.Join(codexHome, "state_5.sqlite")
	const query = `SELECT id, name, title, preview, cwd, git_branch, git_origin_url, model, reasoning_effort,
created_at, updated_at, recency_at, agent_role, agent_nickname, source, is_pinned
FROM threads
WHERE archived != 0 AND TRIM(COALESCE(name, '')) != ''
  AND source IN ('cli', 'vscode', 'appServer', 'exec')
ORDER BY recency_at DESC, id DESC;`
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", database, query).Output()
	if err != nil {
		return nil, fmt.Errorf("read archived Codex project threads: %w", err)
	}
	if strings.TrimSpace(string(output)) == "" {
		return []CodexThreadSummary{}, nil
	}
	var rows []codexSQLiteThread
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("decode archived Codex project threads: %w", err)
	}
	originRoots := make(map[string]string, len(knownOriginRoots))
	for origin, root := range knownOriginRoots {
		originRoots[strings.TrimSpace(origin)] = root
	}
	loadedProjectOrigins := false
	threads := make([]CodexThreadSummary, 0, len(rows))
	for _, row := range rows {
		root := selectedProjectRoot(ctx, row.CWD, projects)
		origin := strings.TrimSpace(row.GitOriginURL)
		if root == "" {
			root = originRoots[origin]
		}
		if root == "" && origin != "" && !loadedProjectOrigins {
			originRoots = projectRootsByGitOrigin(ctx, projects, originRoots)
			loadedProjectOrigins = true
			root = originRoots[origin]
		}
		if strings.TrimSpace(row.ID) == "" || root == "" {
			continue
		}
		threads = append(threads, CodexThreadSummary{
			ID:              row.ID,
			Name:            codexThreadDisplayName(row.Name, row.Title, row.Preview),
			Preview:         truncateRunes(row.Preview, codexPreviewLimit),
			CWD:             row.CWD,
			ProjectRoot:     root,
			Branch:          row.GitBranch,
			Model:           row.Model,
			ReasoningEffort: row.ReasoningEffort,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			RecencyAt:       row.RecencyAt,
			Pinned:          row.Pinned != 0,
			AgentRole:       codexThreadRole(row.AgentRole, row.Title, row.Preview),
			AgentName:       row.AgentNickname,
			Delegated:       codexThreadSourceIsDelegated(row.Source),
		})
	}
	return threads, nil
}

// listCodexCatalogFromSQLite reads the same local Codex state used by
// thread/list. It avoids starting an app-server for the focused Session Wall
// route; if the local schema or sqlite3 binary is unavailable, the caller
// falls back to the protocol client below.
func listCodexCatalogFromSQLite(ctx context.Context, codexHome string, projects []CodexProjectSummary) (CodexCatalog, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" || len(projects) == 0 {
		return CodexCatalog{}, errors.New("Codex SQLite catalog is unavailable")
	}
	database := filepath.Join(codexHome, "state_5.sqlite")
	const query = `SELECT id, name, title, preview, cwd, git_branch, git_origin_url, model, reasoning_effort,
created_at, updated_at, recency_at, agent_role, agent_nickname, source, is_pinned
FROM threads
WHERE archived = 0
  AND (TRIM(COALESCE(name, '')) != '' OR TRIM(COALESCE(title, '')) != '' OR preview <> '')
  AND source IN ('cli', 'vscode', 'appServer', 'exec')
ORDER BY recency_at DESC, id DESC;`
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", database, query).Output()
	if err != nil {
		return CodexCatalog{}, fmt.Errorf("read Codex SQLite catalog: %w", err)
	}
	var rows []codexSQLiteThread
	if err := json.Unmarshal(output, &rows); err != nil {
		return CodexCatalog{}, fmt.Errorf("decode Codex SQLite catalog: %w", err)
	}
	statuses, _ := listCodexThreadStatusesFromSQLite(ctx, codexHome)
	threads := make([]CodexThreadSummary, 0, len(rows))
	rootByCWD := make(map[string]string)
	rootByOrigin := make(map[string]string)
	for _, row := range rows {
		root, known := rootByCWD[row.CWD]
		if !known {
			root = selectedProjectRoot(ctx, row.CWD, projects)
			rootByCWD[row.CWD] = root
		}
		if origin := strings.TrimSpace(row.GitOriginURL); root != "" && origin != "" {
			rootByOrigin[origin] = root
		}
	}
	for _, row := range rows {
		if rootByCWD[row.CWD] != "" || strings.TrimSpace(row.Name) == "" {
			continue
		}
		origin := strings.TrimSpace(row.GitOriginURL)
		if origin != "" && rootByOrigin[origin] == "" {
			rootByOrigin = projectRootsByGitOrigin(ctx, projects, rootByOrigin)
			break
		}
	}
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		root := rootByCWD[row.CWD]
		if root == "" && strings.TrimSpace(row.Name) != "" {
			root = rootByOrigin[strings.TrimSpace(row.GitOriginURL)]
		}
		if root == "" {
			continue
		}
		threads = append(threads, CodexThreadSummary{
			ID:              row.ID,
			Name:            codexThreadDisplayName(row.Name, row.Title, row.Preview),
			Preview:         truncateRunes(row.Preview, codexPreviewLimit),
			CWD:             row.CWD,
			ProjectRoot:     root,
			Status:          statuses[row.ID],
			Branch:          row.GitBranch,
			Model:           row.Model,
			ReasoningEffort: row.ReasoningEffort,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			RecencyAt:       row.RecencyAt,
			Pinned:          row.Pinned != 0,
			AgentRole:       codexThreadRole(row.AgentRole, row.Title, row.Preview),
			AgentName:       row.AgentNickname,
			Delegated:       codexThreadSourceIsDelegated(row.Source),
		})
	}
	markCodexThreadParents(ctx, codexHome, threads)
	markCodexLatestTurnOrigins(ctx, codexHome, threads)
	archivedThreads, err := listCodexArchivedProjectThreadsFromSQLite(ctx, codexHome, projects, rootByOrigin)
	if err != nil {
		return CodexCatalog{}, err
	}
	return CodexCatalog{Threads: threads, ArchivedThreads: archivedThreads, Projects: projects}, nil
}

func newCodexCatalogSession(executable string) *codexCatalogSession {
	return &codexCatalogSession{executable: executable}
}

func (s *codexCatalogSession) resetLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
	s.cmd = nil
	s.cancel = nil
	s.stdin = nil
	s.client = nil
	s.codexHome = ""
	s.loadedThreads = nil
}

func (s *codexCatalogSession) startLocked() error {
	if s.client != nil {
		return nil
	}
	if address := strings.TrimSpace(os.Getenv("SESSION_WALL_CODEX_WS_URL")); address != "" {
		return s.startSharedLocked(address)
	}
	if strings.TrimSpace(s.executable) == "" {
		return errors.New("Codex executable is unavailable")
	}

	processContext, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processContext, s.executable, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("open Codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return fmt.Errorf("open Codex stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return fmt.Errorf("start Codex app-server: %w", err)
	}

	client := newCodexCatalogClient(stdin, stdout)
	initializeResult, err := client.call("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "multica-session-card-wall",
			"title":   "Multica Session Card Wall",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err == nil {
		err = client.notify("initialized")
	}
	if err != nil {
		cancel()
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	var initialize struct {
		CodexHome string `json:"codexHome"`
	}
	_ = json.Unmarshal(initializeResult, &initialize)

	s.cmd = cmd
	s.cancel = cancel
	s.stdin = stdin
	s.client = client
	s.codexHome = initialize.CodexHome
	s.loadedThreads = make(map[string]bool)
	return nil
}

func (s *codexCatalogSession) warm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		s.resetLocked()
	}
}

func (s *codexCatalogSession) models(ctx context.Context) ([]CodexModelSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return nil, err
	}
	models, err := listAllCodexModels(s.client.call)
	if err != nil {
		s.resetLocked()
		return nil, err
	}
	return models, nil
}

func (s *codexCatalogSession) list(ctx context.Context, projectsFile string) (CodexCatalog, error) {
	if strings.TrimSpace(s.executable) == "" {
		return CodexCatalog{}, errors.New("Codex executable is unavailable")
	}
	var selectedProjects []CodexProjectSummary
	if strings.TrimSpace(projectsFile) != "" {
		var err error
		selectedProjects, err = readCodexProjectSelection(projectsFile)
		if err != nil {
			return CodexCatalog{}, err
		}
		if catalog, err := listCodexCatalogFromSQLite(ctx, defaultCodexHome(), selectedProjects); err == nil {
			return catalog, nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return CodexCatalog{}, err
	}

	var (
		threads  []CodexThreadSummary
		projects []CodexProjectSummary
		err      error
	)
	if len(selectedProjects) > 0 {
		var cwdRoots map[string]string
		cwdRoots, err = selectedProjectCWDs(ctx, s.codexHome, selectedProjects)
		if err == nil {
			threads, err = listSelectedCodexThreads(s.client.call, cwdRoots)
		}
		projects = selectedProjects
	} else {
		threads, err = listAllCodexThreads(s.client.call)
		if err == nil {
			projects, err = listAllCodexProjects(s.client.call)
			if err != nil {
				projects = []CodexProjectSummary{}
				err = nil
			}
		}
	}
	if err != nil {
		s.resetLocked()
		return CodexCatalog{}, err
	}
	if len(selectedProjects) == 0 {
		markWorktreeProjectRoots(ctx, threads)
	}
	markPinnedThreads(ctx, s.codexHome, threads)
	markCodexThreadParents(ctx, s.codexHome, threads)
	markCodexLatestTurnOrigins(ctx, s.codexHome, threads)
	return CodexCatalog{Threads: threads, Projects: projects}, nil
}

func (s *codexCatalogSession) transcript(ctx context.Context, threadID string) (CodexThreadTranscript, error) {
	if transcript, err := readCodexThreadTranscriptFromSQLite(ctx, defaultCodexHome(), threadID); err == nil {
		return transcript, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return CodexThreadTranscript{}, err
	}
	transcript, err := readCodexThreadTranscript(s.client.call, threadID)
	if err != nil {
		s.resetLocked()
		return CodexThreadTranscript{}, err
	}
	if statuses, statusErr := listCodexThreadStatusesFromSQLite(ctx, s.codexHome); statusErr == nil {
		if status := statuses[threadID]; status != "" {
			transcript.Status = status
		}
	}
	return transcript, nil
}

func (s *codexCatalogSession) restore(ctx context.Context, threadID string) (CodexThreadRestoreResult, error) {
	if err := ctx.Err(); err != nil {
		return CodexThreadRestoreResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return CodexThreadRestoreResult{}, err
	}
	result, err := unarchiveCodexThread(s.client.call, threadID)
	if err != nil {
		s.resetLocked()
		return CodexThreadRestoreResult{}, err
	}
	return result, nil
}

func (s *codexCatalogSession) enqueueMessage(threadID, message, clientUserMessageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return "", err
	}
	submissionID, err := queueCodexThreadMessage(s.client.call, threadID, message, clientUserMessageID)
	if err != nil {
		s.resetLocked()
		return "", err
	}
	// Queue writers are deliberately short-lived. Keeping this app-server warm
	// can retain the thread writer and prevent the executor from resuming it.
	s.resetLocked()
	return submissionID, nil
}

func (s *codexCatalogSession) executeQueuedMessagesLocked(
	threadID string,
	optionsFor func(string) CodexMessageOptions,
	completed func(string),
) {
	if err := s.startLocked(); err != nil {
		return
	}
	resume := !s.loadedThreads[threadID]
	for {
		submissionIDs, err := listCodexQueuedMessageIDs(s.client.call, threadID)
		if err != nil {
			s.resetLocked()
			return
		}
		if len(submissionIDs) == 0 {
			// Release the thread writer once the web queue is empty so opening
			// the same task in Codex never collides with an idle Session Wall.
			s.resetLocked()
			return
		}
		submissionID := submissionIDs[0]
		turnID, err := startCodexQueuedMessage(
			s.client.call, threadID, submissionID, resume, optionsFor(submissionID),
		)
		if err != nil {
			s.resetLocked()
			return
		}
		s.loadedThreads[threadID] = true
		resume = false
		completed(submissionID)
		s.client.drainTurn(turnID)
	}
}

func (s *codexCatalogSession) executeQueuedMessages(
	threadID string,
	optionsFor func(string) CodexMessageOptions,
	completed func(string),
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeQueuedMessagesLocked(threadID, optionsFor, completed)
}

func (s *codexCatalogSession) tryStartMessage(
	threadID, message, clientUserMessageID string,
	options CodexMessageOptions,
	optionsFor func(string) CodexMessageOptions,
	completed func(string),
) (string, bool, error) {
	if !s.mu.TryLock() {
		return "", false, nil
	}
	if err := s.startLocked(); err != nil {
		s.mu.Unlock()
		return "", true, err
	}
	if hostExecutable, err := os.Executable(); err == nil {
		options.RouterToolConfig = wallRouterToolConfig(s.client.call, s.executable, hostExecutable, threadID)
	}
	turnID, err := startCodexThreadMessage(
		s.client.call, threadID, message, clientUserMessageID,
		!s.loadedThreads[threadID], options,
	)
	if err != nil {
		s.resetLocked()
		s.mu.Unlock()
		return "", true, err
	}
	s.loadedThreads[threadID] = true
	s.activeMu.Lock()
	s.activeTurn, s.activeClient = turnID, s.client
	s.activeMu.Unlock()
	go func() {
		s.client.drainTurn(turnID)
		s.activeMu.Lock()
		s.activeTurn, s.activeClient = "", nil
		s.activeMu.Unlock()
		s.resetLocked()
		s.mu.Unlock()
	}()
	return turnID, true, nil
}

func newCodexThreadMessageSession(executable string) *codexThreadMessageSession {
	return &codexThreadMessageSession{
		queue:    newCodexCatalogSession(executable),
		executor: newCodexCatalogSession(executable),
		options:  make(map[string]CodexMessageOptions),
	}
}

func newCodexMessageSession(executable string) *codexMessageSession {
	return &codexMessageSession{
		executable: executable,
		threads:    make(map[string]*codexThreadMessageSession),
	}
}

func (s *codexMessageSession) thread(threadID string) *codexThreadMessageSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.threads[threadID]; session != nil {
		return session
	}
	session := newCodexThreadMessageSession(s.executable)
	s.threads[threadID] = session
	return session
}

func (s *codexThreadMessageSession) messageOptions(submissionID string) CodexMessageOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options[submissionID]
}

func (s *codexThreadMessageSession) completeMessage(submissionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.options, submissionID)
}

func (s *codexThreadMessageSession) sendMessage(
	ctx context.Context,
	threadID, message string,
	options CodexMessageOptions,
) (CodexMessageResult, error) {
	if err := ctx.Err(); err != nil {
		return CodexMessageResult{}, err
	}
	clientUserMessageID := uuid.NewString()
	turnID, acquired, err := s.executor.tryStartMessage(
		threadID, message, clientUserMessageID, options,
		s.messageOptions, s.completeMessage,
	)
	if err == nil && acquired {
		return CodexMessageResult{MessageID: clientUserMessageID, TurnID: turnID, State: "started"}, nil
	}
	if err != nil && isCodexActiveWriterError(err) {
		return CodexMessageResult{}, errors.New("Codex 客户端占用了该任务的连接，本条消息未发送。请在客户端结束或停止该任务后重试；输入内容已保留")
	}
	if !acquired {
		return CodexMessageResult{}, errors.New("该任务正在处理上一轮，本条消息未发送。请等待结束或点击停止后重试")
	}
	return CodexMessageResult{}, err
}

func (s *codexMessageSession) sendMessage(
	ctx context.Context,
	threadID, message string,
	options CodexMessageOptions,
) (CodexMessageResult, error) {
	return s.thread(threadID).sendMessage(ctx, threadID, message, options)
}

func listCodexCatalogWithProjects(ctx context.Context, executable, projectsFile string) (CodexCatalog, error) {
	if strings.TrimSpace(executable) == "" {
		return CodexCatalog{}, errors.New("Codex executable is unavailable")
	}
	var selectedProjects []CodexProjectSummary
	if strings.TrimSpace(projectsFile) != "" {
		var err error
		selectedProjects, err = readCodexProjectSelection(projectsFile)
		if err != nil {
			return CodexCatalog{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, codexThreadListTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return CodexCatalog{}, fmt.Errorf("open Codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return CodexCatalog{}, fmt.Errorf("open Codex stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return CodexCatalog{}, fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	client := newCodexCatalogClient(stdin, stdout)
	initializeResult, err := client.call("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "multica-session-card-wall",
			"title":   "Multica Session Card Wall",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return CodexCatalog{}, err
	}
	if err := client.notify("initialized"); err != nil {
		return CodexCatalog{}, fmt.Errorf("notify Codex initialized: %w", err)
	}
	var initialize struct {
		CodexHome string `json:"codexHome"`
	}
	_ = json.Unmarshal(initializeResult, &initialize)
	var threads []CodexThreadSummary
	var projects []CodexProjectSummary
	if len(selectedProjects) > 0 {
		var cwdRoots map[string]string
		cwdRoots, err = selectedProjectCWDs(ctx, initialize.CodexHome, selectedProjects)
		if err == nil {
			threads, err = listSelectedCodexThreads(client.call, cwdRoots)
		}
		projects = selectedProjects
	} else {
		threads, err = listAllCodexThreads(client.call)
		if err == nil {
			projects, err = listAllCodexProjects(client.call)
			if err != nil {
				projects = []CodexProjectSummary{}
				err = nil
			}
		}
	}
	if err != nil {
		return CodexCatalog{}, err
	}
	if len(selectedProjects) == 0 {
		markWorktreeProjectRoots(ctx, threads)
	}
	markPinnedThreads(ctx, initialize.CodexHome, threads)
	return CodexCatalog{Threads: threads, Projects: projects}, nil
}

func listCodexCatalog(ctx context.Context, executable string) (CodexCatalog, error) {
	return listCodexCatalogWithProjects(ctx, executable, "")
}

func localCatalogOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func codexThreadsHandler(list func(context.Context) (CodexCatalog, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		catalog, err := list(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(catalog)
	})
}

// NewCodexThreadCatalogHandler exposes the read-only catalog without starting
// the task daemon. The focused Session Wall build uses this local-only surface.
func NewCodexThreadCatalogHandler(executable string) http.Handler {
	session := newCodexCatalogSession(executable)
	return codexThreadsHandler(func(ctx context.Context) (CodexCatalog, error) {
		return session.list(ctx, "")
	})
}

// NewCodexThreadCatalogHandlerWithProjects limits Codex reads to the project IDs
// stored in projectsFile. The file is re-read for each request so the selection
// can change without restarting the local service.
func NewCodexThreadCatalogHandlerWithProjects(executable, projectsFile string) http.Handler {
	session := newCodexCatalogSession(executable)
	return codexThreadsHandler(func(ctx context.Context) (CodexCatalog, error) {
		return session.list(ctx, projectsFile)
	})
}

func codexThreadStatusesHandler(
	list func(context.Context) (map[string]string, error),
	archivedLists ...func(context.Context) ([]string, error),
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		statuses, err := list(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		archivedThreadIDs := []string{}
		if len(archivedLists) > 0 {
			archivedThreadIDs, err = archivedLists[0](r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		requestedThreadIDs := r.URL.Query()["thread_id"]
		if len(requestedThreadIDs) > 0 {
			requested := make(map[string]bool, len(requestedThreadIDs))
			for _, threadID := range requestedThreadIDs {
				threadID = strings.TrimSpace(threadID)
				if !codexThreadIDPattern.MatchString(threadID) {
					http.Error(w, "invalid thread id", http.StatusBadRequest)
					return
				}
				requested[threadID] = true
			}
			for threadID := range statuses {
				if !requested[threadID] {
					delete(statuses, threadID)
				}
			}
			filteredArchived := archivedThreadIDs[:0]
			for _, threadID := range archivedThreadIDs {
				if requested[threadID] {
					filteredArchived = append(filteredArchived, threadID)
				}
			}
			archivedThreadIDs = filteredArchived
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statuses":            statuses,
			"archived_thread_ids": archivedThreadIDs,
		})
	})
}

// NewCodexThreadStatusesHandler exposes the latest persisted turn state for
// lightweight polling without reloading the full conversation catalog.
func NewCodexThreadStatusesHandler() http.Handler {
	return codexThreadStatusesHandler(
		func(ctx context.Context) (map[string]string, error) {
			return listCodexThreadStatusesFromSQLite(ctx, defaultCodexHome())
		},
		func(ctx context.Context) ([]string, error) {
			return listCodexArchivedThreadIDsFromSQLite(ctx, defaultCodexHome())
		},
	)
}

var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var codexModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var codexReasoningEffortPattern = regexp.MustCompile(`^(none|minimal|low|medium|high|xhigh|max|ultra)$`)

func codexThreadTranscriptHandler(read func(context.Context, string) (CodexThreadTranscript, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
		if !codexThreadIDPattern.MatchString(threadID) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		transcript, err := read(r.Context(), threadID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transcript)
	})
}

// NewCodexThreadTranscriptHandler exposes a local-only, read-only view of the
// most recent Codex turns for the Session Wall conversation drawer.
func NewCodexThreadTranscriptHandler(executable string) http.Handler {
	session := newCodexCatalogSession(executable)
	return codexThreadTranscriptHandler(session.transcript)
}

func codexRestoreThreadHandler(
	restore func(context.Context, string) (CodexThreadRestoreResult, error),
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !localCatalogOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
		if !codexThreadIDPattern.MatchString(threadID) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		result, err := restore(r.Context(), threadID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}

// NewCodexThreadRestoreHandler unarchives a remembered local Codex task. It
// restores the task record only; Codex may already have removed its worktree.
func NewCodexThreadRestoreHandler(executable string) http.Handler {
	session := newCodexCatalogSession(executable)
	return codexRestoreThreadHandler(session.restore)
}

func codexModelsHandler(list func(context.Context) ([]CodexModelSummary, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		models, err := list(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
}

// NewCodexModelsHandler exposes the same local model and reasoning catalog
// maintained by Codex without starting another app-server during page load.
func NewCodexModelsHandler(_ string) http.Handler {
	return codexModelsHandler(func(context.Context) ([]CodexModelSummary, error) {
		return readCodexModelsCache(filepath.Join(defaultCodexHome(), "models_cache.json"))
	})
}

func codexSendMessageHandler(
	send func(context.Context, string, string, CodexMessageOptions) (CodexMessageResult, error),
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !localCatalogOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
		if !codexThreadIDPattern.MatchString(threadID) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, codexMessageTextMax*4+1))
		if err != nil {
			http.Error(w, "invalid message", http.StatusBadRequest)
			return
		}
		if len(body) > codexMessageTextMax*4 {
			http.Error(w, "message is too long", http.StatusRequestEntityTooLarge)
			return
		}
		options := CodexMessageOptions{
			Interrupt:       r.URL.Query().Get("action") == "interrupt",
			Model:           strings.TrimSpace(r.URL.Query().Get("model")),
			ReasoningEffort: strings.TrimSpace(r.URL.Query().Get("reasoning_effort")),
		}
		message := strings.TrimSpace(string(body))
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			var request struct {
				Message string `json:"message"`
				CodexMessageOptions
			}
			if err := json.Unmarshal(body, &request); err != nil {
				http.Error(w, "invalid message", http.StatusBadRequest)
				return
			}
			message = strings.TrimSpace(request.Message)
			options.Model = strings.TrimSpace(request.Model)
			options.ReasoningEffort = strings.TrimSpace(request.ReasoningEffort)
		}
		if message == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}
		if len([]byte(message)) > codexMessageTextMax {
			http.Error(w, "message is too long", http.StatusRequestEntityTooLarge)
			return
		}
		if options.Model != "" && !codexModelIDPattern.MatchString(options.Model) {
			http.Error(w, "invalid model", http.StatusBadRequest)
			return
		}
		if options.ReasoningEffort != "" && !codexReasoningEffortPattern.MatchString(options.ReasoningEffort) {
			http.Error(w, "invalid reasoning effort", http.StatusBadRequest)
			return
		}
		result, err := send(r.Context(), threadID, message, options)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		validState := result.State == "started" || result.State == "queued"
		if r.URL.Query().Get("action") == "client-send" {
			validState = validState || result.State == "submitted" || result.State == "unknown" || result.State == "completed" || result.State == "interrupted" || result.State == "failed"
		}
		if result.MessageID == "" || !validState || (result.State == "started" && result.TurnID == "") {
			http.Error(w, "Codex did not confirm the message", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(result)
	})
}

// NewCodexSendMessageHandler owns web turns and their interrupt connection.
func NewCodexSendMessageHandler(executable string) http.Handler {
	session := newCodexMessageSession(executable)
	send := codexSendMessageHandler(session.sendMessage)
	clientSend := codexClientFallbackHandler(func(method string, params any) (json.RawMessage, error) {
		return sharedDesktopCall(executable, method, params)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Query().Get("action"), "client-") {
			clientSend.ServeHTTP(w, r)
			return
		}
		if r.URL.Query().Get("action") != "stop" {
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
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("thread_id")
		if !codexThreadIDPattern.MatchString(id) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := session.thread(id).executor.interrupt(ctx, id); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *codexCatalogSession) interrupt(ctx context.Context, threadID string) error {
	s.activeMu.Lock()
	c, turnID := s.activeClient, s.activeTurn
	if c == nil || turnID == "" {
		s.activeMu.Unlock()
		return errors.New("没有可停止的网页运行轮次；如果任务由 Codex 客户端启动，请在客户端停止")
	}
	err := json.NewEncoder(c.stdin).Encode(map[string]any{"id": "wall-interrupt", "method": "turn/interrupt", "params": map[string]any{"threadId": threadID, "turnId": turnID}})
	s.activeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case err := <-c.interruptReply:
		return err
	case <-ctx.Done():
		return errors.New("停止请求尚未获得确认，请检查最新输出后重试")
	}
}

func openCodexThread(ctx context.Context, target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", target)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.CommandContext(ctx, "xdg-open", target)
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("open Codex thread: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func codexOpenThreadHandler(open func(context.Context, string) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !localCatalogOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 128))
		if err != nil {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		threadID := strings.TrimSpace(string(body))
		if !codexThreadIDPattern.MatchString(threadID) {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		if err := open(r.Context(), "codex://threads/"+threadID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// NewCodexOpenThreadHandler hands a validated local thread deep link to the OS.
func NewCodexOpenThreadHandler() http.Handler {
	return codexOpenThreadHandler(openCodexThread)
}

func (d *Daemon) codexThreadsHandler() http.Handler {
	return codexThreadsHandler(func(ctx context.Context) (CodexCatalog, error) {
		entry, ok := d.agents()["codex"]
		if !ok {
			return CodexCatalog{}, errors.New("Codex runtime is not available on this daemon")
		}
		entry, _ = d.resolveAgentEntry(ctx, "codex", entry)
		return listCodexCatalog(ctx, entry.Path)
	})
}
