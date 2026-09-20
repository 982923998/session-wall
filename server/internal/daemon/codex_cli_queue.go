package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ownerProbe func(context.Context, string, ...string) ([]byte, error)

func desktopOwnsThread(ctx context.Context, home, threadID string) bool {
	path := filepath.Join(home, "thread-writer-locks", threadID+".lock")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return probeDesktopOwner(ctx, path, func(ctx context.Context, command string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, command, args...).Output()
	})
}

func probeDesktopOwner(ctx context.Context, path string, run ownerProbe) bool {
	output, err := run(ctx, "lsof", "-t", path)
	if err != nil {
		return false
	}
	for _, pid := range strings.Fields(string(output)) {
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		parent, err := run(ctx, "ps", "-p", pid, "-o", "ppid=")
		if err != nil {
			continue
		}
		ppid := strings.TrimSpace(string(parent))
		if _, err := strconv.Atoi(ppid); err != nil {
			continue
		}
		command, err := run(ctx, "ps", "-p", ppid, "-o", "comm=")
		if err == nil && strings.TrimSpace(string(command)) == "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT" {
			return true
		}
	}
	return false
}

// Queue only after resume was rejected, before any turn input was submitted.
// The current writer consumes the official queue; we never resume it here.
func queueViaCodexCLI(ctx context.Context, executable, home, threadID, message string, options CodexMessageOptions) (CodexMessageResult, error) {
	if options.Interrupt {
		return CodexMessageResult{}, errors.New("任务由客户端管理；请在客户端停止，本条消息未发送")
	}
	if err := checkQueuedModelSettings(ctx, home, threadID, options); err != nil {
		return CodexMessageResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable, "queue", "--thread", threadID, "--message", message).Output()
	if err != nil {
		return CodexMessageResult{}, errors.New("客户端队列投递失败或结果未确认；请先检查客户端再重试，输入已保留")
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) != 6 || fields[0] != "Queued" || fields[1] != "message" || fields[3] != "for" || fields[4] != "thread" || !codexThreadIDPattern.MatchString(fields[2]) || strings.TrimSuffix(fields[5], ".") != threadID {
		// Output format drift is uncertain delivery, not permission to retry.
		return CodexMessageResult{}, errors.New("客户端队列回执格式未识别；可能已投递，请检查客户端，勿重复发送")
	}
	return CodexMessageResult{MessageID: fields[2], State: "queued", Delivery: "client_queue"}, nil
}

func checkQueuedModelSettings(ctx context.Context, home, threadID string, options CodexMessageOptions) error {
	if options.Model == "" && options.ReasoningEffort == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	query := "SELECT model, reasoning_effort FROM threads WHERE id = " + sqliteStringLiteral(threadID) + ";"
	output, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", filepath.Join(home, "state_5.sqlite"), query).Output()
	var rows []struct {
		Model  string `json:"model"`
		Effort string `json:"reasoning_effort"`
	}
	if err != nil || json.Unmarshal(output, &rows) != nil || len(rows) != 1 {
		return errors.New("无法核对客户端模型设置，本条消息未投递；输入已保留")
	}
	if (options.Model != "" && options.Model != rows[0].Model) || (options.ReasoningEffort != "" && options.ReasoningEffort != rows[0].Effort) {
		return errors.New("任务由客户端管理，队列沿用客户端模型和推理程度；请将网页与客户端设置保持一致后发送，输入已保留")
	}
	return nil
}
