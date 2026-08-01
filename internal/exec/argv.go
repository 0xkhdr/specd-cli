// Package exec is the one place a harness operation turns argv into a process.
// It never interprets a shell: every element of Argv reaches the child as one
// literal argument, so a metacharacter is data and can start no sibling command.
package exec

import (
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultTimeout     = 2 * time.Minute
	MaximumTimeout     = 10 * time.Minute
	DefaultOutputLimit = 32 * 1024
	MaximumOutputLimit = 1024 * 1024

	TimeoutExitCode = 124
)

// Request is one bounded direct execution.
type Request struct {
	// Dir is the absolute working directory the process runs in.
	Dir string
	// Argv is the executable and its literal arguments. No element is parsed.
	Argv []string
	// Timeout bounds wall-clock time; OutputLimit bounds captured bytes.
	Timeout     time.Duration
	OutputLimit int
}

type Result struct {
	ExitCode        int
	TimedOut        bool
	Stdout, Stderr  string
	StdoutTruncated bool
	StderrTruncated bool
}

// Command builds the child process without a shell. It fails closed on empty
// argv, an embedded NUL, or a non-absolute working directory.
func Command(ctx context.Context, request Request) (*osexec.Cmd, error) {
	if len(request.Argv) == 0 || strings.TrimSpace(request.Argv[0]) == "" {
		return nil, errors.New("argv is required; next: declare the executable and its arguments")
	}
	for _, value := range request.Argv {
		if strings.IndexByte(value, 0) >= 0 {
			return nil, errors.New("argv contains NUL; next: remove the NUL byte from the argument")
		}
	}
	if strings.TrimSpace(request.Dir) == "" || !filepath.IsAbs(request.Dir) {
		return nil, errors.New("working directory must be absolute; next: pass the resolved project path")
	}
	command := osexec.CommandContext(ctx, request.Argv[0], request.Argv[1:]...)
	command.Dir = request.Dir
	// No Env, no Stdin, no shell: the child inherits nothing this package was
	// not given.
	command.Stdin = nil
	return command, nil
}

// Run executes bounded argv and returns bounded output. Output past the limit
// is dropped rather than buffered, so a chatty process cannot exhaust memory.
func Run(ctx context.Context, request Request) (Result, error) {
	timeout, limit, err := bounds(request)
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command, err := Command(runCtx, request)
	if err != nil {
		return Result{}, err
	}
	stdout, stderr := &boundedBuffer{limit: limit}, &boundedBuffer{limit: limit}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = time.Second
	runErr := command.Run()
	result := Result{
		Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		result.ExitCode, result.TimedOut = TimeoutExitCode, true
	case runErr == nil:
		result.ExitCode = 0
	default:
		var exitErr *osexec.ExitError
		if !errors.As(runErr, &exitErr) {
			return Result{}, fmt.Errorf("start process: %w; next: repair the declared command", runErr)
		}
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode < 0 {
			result.ExitCode = 1
		}
	}
	return result, nil
}

func bounds(request Request) (time.Duration, int, error) {
	timeout := request.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 || timeout > MaximumTimeout {
		return 0, 0, fmt.Errorf("timeout must be between 1ns and %s; next: lower the declared timeout", MaximumTimeout)
	}
	limit := request.OutputLimit
	if limit == 0 {
		limit = DefaultOutputLimit
	}
	if limit < 0 || limit > MaximumOutputLimit {
		return 0, 0, fmt.Errorf("output limit must be between 1 and %d bytes; next: lower the declared output limit", MaximumOutputLimit)
	}
	return timeout, limit, nil
}

type boundedBuffer struct {
	limit     int
	captured  []byte
	truncated bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	if remaining := buffer.limit - len(buffer.captured); remaining > 0 {
		take := min(len(value), remaining)
		buffer.captured = append(buffer.captured, value[:take]...)
		buffer.truncated = buffer.truncated || take < len(value)
	} else {
		buffer.truncated = true
	}
	return len(value), nil
}

func (buffer *boundedBuffer) String() string { return string(buffer.captured) }
