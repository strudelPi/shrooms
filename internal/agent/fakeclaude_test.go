package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The tests run the production session and HTTP code against a stand-in for
// `claude -p`: this test binary, re-executed with FAKE_CLAUDE=1. It speaks the
// stream-json protocol as observed from Claude Code 2.1.287 (docs/agents.md):
//
//	a system/init message carrying the session id (the resumed one, if
//	  --resume was given), then for each user turn:
//	"run <x>"  → a tool_use, a control_request can_use_tool, and after the
//	             control_response either a tool result or the denial reason;
//	"slow"     → a turn that only ends when interrupted;
//	anything   → an assistant text echoing it;
//	each turn ending with a result. EOF on stdin ends the process.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CLAUDE") == "1" {
		fakeClaude()
		return
	}
	os.Exit(m.Run())
}

func fakeClaude() {
	out := json.NewEncoder(os.Stdout)
	emit := func(v any) { out.Encode(v) }

	id, resumed := "fake-session-1", false
	for i, a := range os.Args {
		if a == "--resume" && i+1 < len(os.Args) {
			id, resumed = os.Args[i+1], true
		}
	}
	if !hasArgs(os.Args, "--permission-prompt-tool", "stdio") {
		// Without it Claude Code denies every prompt on its own; a session
		// started that way could never be approved from a phone.
		emit(map[string]any{"type": "result", "subtype": "error", "result": "no permission prompt tool"})
		os.Exit(3)
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": id, "resumed": resumed, "cwd": mustWd()})

	in := bufio.NewScanner(os.Stdin)
	next := func() map[string]any {
		if !in.Scan() {
			os.Exit(0)
		}
		var m map[string]any
		json.Unmarshal(in.Bytes(), &m)
		return m
	}
	text := func(s string) {
		emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": s}}}})
	}
	result := func(sub string) { emit(map[string]any{"type": "result", "subtype": sub, "session_id": id}) }

	for {
		m := next()
		if m["type"] != "user" {
			continue
		}
		content, _ := m["message"].(map[string]any)["content"].(string)
		switch {
		case strings.HasPrefix(content, "run "):
			cmd := strings.TrimPrefix(content, "run ")
			emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
				"content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash",
					"input": map[string]any{"command": cmd}}}}})
			emit(map[string]any{"type": "control_request", "request_id": "req-1", "request": map[string]any{
				"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": cmd},
				"description": "Run " + cmd}})
			var resp map[string]any
			for {
				resp = next()
				if resp["type"] == "control_response" {
					break
				}
			}
			r := resp["response"].(map[string]any)
			if r["request_id"] != "req-1" {
				text("answered the wrong prompt")
			}
			inner := r["response"].(map[string]any)
			if inner["behavior"] == "allow" {
				got := inner["updatedInput"].(map[string]any)["command"]
				emit(map[string]any{"type": "user", "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1",
						"content": fmt.Sprintf("ran %v", got)}}}})
				text("done")
			} else {
				text(fmt.Sprintf("denied: %v", inner["message"]))
			}
			result("success")
		case content == "slow":
			for {
				c := next()
				if c["type"] == "control_request" {
					if r, _ := c["request"].(map[string]any); r["subtype"] == "interrupt" {
						break
					}
				}
			}
			result("error_during_execution")
		default:
			text("echo: " + content)
			result("success")
		}
	}
}

func hasArgs(args []string, k, v string) bool {
	for i := range args {
		if args[i] == k && i+1 < len(args) && args[i+1] == v {
			return true
		}
	}
	return false
}

func mustWd() string {
	wd, _ := os.Getwd()
	return wd
}

// fakeClaudeBin is a `claude` that runs this binary as the fake.
func fakeClaudeBin(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLAUDE", "1")
	return self
}
