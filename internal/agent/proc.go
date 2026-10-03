// Package agent runs Claude Code sessions and serves them to the owner's other
// devices over the mesh (docs/agents.md).
//
// A session is a name and a directory — what `cl` keyed its tmux sessions on —
// plus the Claude Code conversation id that lets it be resumed. While in use it
// has one `claude -p` process speaking stream-json; idle, it has none, and the
// next message resumes it.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
)

// proc is one running `claude -p` process.
//
// Every line it writes is a JSON message (stream-json), handed to out in
// order; out is closed when the process has gone, and err then says why.
type proc struct {
	cmd *exec.Cmd

	mu    sync.Mutex // serialises writes: lines must not interleave
	stdin io.WriteCloser

	out  chan json.RawMessage
	done chan struct{}
	err  error
}

// claudeArgs is how a session's process is started.
//
// --permission-prompt-tool stdio is what makes permission prompts reach us as
// control requests. Without it, and with --print, anything that would prompt
// is denied automatically and the model is told the user refused (observed on
// Claude Code 2.1.287).
func claudeArgs(resume string) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-prompt-tool", "stdio",
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return args
}

func startProc(ctx context.Context, log *slog.Logger, bin, dir, resume string) (*proc, error) {
	cmd := exec.CommandContext(ctx, bin, claudeArgs(resume)...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	p := &proc{cmd: cmd, stdin: stdin, out: make(chan json.RawMessage, 64), done: make(chan struct{})}

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Debug("claude stderr", "line", sc.Text())
		}
	}()
	go func() {
		defer close(p.done)
		sc := bufio.NewScanner(stdout)
		// A tool result can be a whole file; the default 64 KiB line limit
		// would end the session on the first large read.
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if !json.Valid(line) {
				log.Debug("claude wrote a line that is not JSON", "line", string(line))
				continue
			}
			p.out <- append(json.RawMessage(nil), line...)
		}
		close(p.out)
		werr := cmd.Wait()
		switch {
		case sc.Err() != nil:
			p.err = sc.Err()
		case werr != nil:
			p.err = werr
		}
	}()
	return p, nil
}

// write sends one message to the process.
func (p *proc) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin == nil {
		return errors.New("the session's process has stopped taking input")
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// close ends the input, which is how a stream-json process is asked to finish:
// it completes the turn in hand and exits.
func (p *proc) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}
}
