package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	DefaultTimeout     = 2 * time.Minute
	MaximumTimeout     = 10 * time.Minute
	DefaultOutputLimit = 32 * 1024
	MaximumOutputLimit = 1024 * 1024

	TimeoutExitCode     = 124
	ZeroMatchExitCode   = 126
	InterruptedExitCode = 130
)

type Request struct {
	Dir         string
	Argv        []string
	Shell       string
	Timeout     time.Duration
	OutputLimit int
}

type Result struct {
	CommandDigest   string `json:"command_digest"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at"`
	ExitCode        int    `json:"exit_code"`
	Passed          bool   `json:"passed"`
	TimedOut        bool   `json:"timed_out"`
	Interrupted     bool   `json:"interrupted"`
	NonVacuous      bool   `json:"non_vacuous"`
	GoRunSelector   bool   `json:"go_run_selector"`
	ZeroMatch       bool   `json:"zero_match"`
	StdoutDigest    string `json:"stdout_digest"`
	StderrDigest    string `json:"stderr_digest"`
	StdoutExcerpt   string `json:"stdout_excerpt,omitempty"`
	StderrExcerpt   string `json:"stderr_excerpt,omitempty"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

// CommandDigest returns the canonical identity of the exact argv or explicit
// shell command that Run will execute.
func CommandDigest(request Request) (string, error) {
	_, _, mode, err := command(request)
	if err != nil {
		return "", err
	}
	return commandDigest(mode, request.Argv, request.Shell), nil
}

// ParseCommand preserves authored argv unless shell semantics are explicit.
func ParseCommand(authored string) (Request, error) {
	if strings.HasPrefix(authored, "shell:") {
		command := strings.TrimSpace(strings.TrimPrefix(authored, "shell:"))
		if command == "" {
			return Request{}, errors.New("explicit shell command is empty")
		}
		return Request{Shell: command}, nil
	}
	argv, err := splitArgv(authored)
	if err != nil {
		return Request{}, err
	}
	return Request{Argv: argv}, nil
}

func splitArgv(value string) ([]string, error) {
	var result []string
	var word strings.Builder
	quote := rune(0)
	escaped, started := false, false
	flush := func() {
		if started {
			result = append(result, word.String())
			word.Reset()
			started = false
		}
	}
	for _, character := range value {
		if escaped {
			word.WriteRune(character)
			escaped, started = false, true
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped, started = true, true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				word.WriteRune(character)
			}
			started = true
			continue
		}
		if character == '\'' || character == '"' {
			quote, started = character, true
			continue
		}
		if strings.ContainsRune("|&;<>()$`*?[]{}~\n\r", character) {
			return nil, fmt.Errorf("shell operator %q requires explicit shell: semantics", character)
		}
		if character == ' ' || character == '\t' {
			flush()
			continue
		}
		word.WriteRune(character)
		started = true
	}
	if escaped || quote != 0 {
		return nil, errors.New("malformed quoted argv")
	}
	flush()
	if len(result) == 0 || strings.TrimSpace(result[0]) == "" {
		return nil, errors.New("argv is empty")
	}
	return result, nil
}

func Run(ctx context.Context, request Request) (Result, error) {
	name, arguments, mode, err := command(request)
	if err != nil {
		return Result{}, err
	}
	dir, err := canonicalDir(request.Dir)
	if err != nil {
		return Result{}, err
	}
	timeout := request.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 || timeout > MaximumTimeout {
		return Result{}, fmt.Errorf("verify timeout must be between 1ns and %s", MaximumTimeout)
	}
	limit := request.OutputLimit
	if limit == 0 {
		limit = DefaultOutputLimit
	}
	if limit < 0 || limit > MaximumOutputLimit {
		return Result{}, fmt.Errorf("verify output limit must be between 1 and %d bytes", MaximumOutputLimit)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now().UTC()
	result := Result{
		CommandDigest: commandDigest(mode, request.Argv, request.Shell),
		StartedAt:     started.Format(time.RFC3339Nano),
		NonVacuous:    true,
	}
	stdout := newBoundedDigest(limit)
	stderr := newBoundedDigest(limit)
	process := exec.CommandContext(runCtx, name, arguments...)
	process.Dir = dir
	process.WaitDelay = time.Second
	configureProcess(process)
	var terminationFailure atomic.Value
	process.Cancel = func() error {
		err := terminateProcessTree(process)
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			terminationFailure.Store(err)
		}
		return err
	}
	process.Stdout = stdout
	process.Stderr = stderr
	// Start and wait separately, because the tree must be bound to the
	// platform's termination primitive between the two. A start that fails is
	// classified below exactly as before: an expired context is an interruption,
	// anything else is a transport failure.
	runErr := process.Start()
	if runErr == nil {
		// A tree that cannot be bounded is killed rather than run unbounded: an
		// unterminable verification is worse than a refused one.
		if err := afterStart(process); err != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			return Result{}, fmt.Errorf("bound verification process tree: %w", err)
		}
		defer releaseProcess(process)
		runErr = process.Wait()
	}
	if err, _ := terminationFailure.Load().(error); err != nil {
		return Result{}, fmt.Errorf("terminate verification process tree: %w", err)
	}
	result.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.StdoutDigest, result.StderrDigest = stdout.digest(), stderr.digest()
	result.StdoutExcerpt, result.StderrExcerpt = stdout.excerpt(), stderr.excerpt()
	result.StdoutTruncated, result.StderrTruncated = stdout.truncated, stderr.truncated

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		result.ExitCode, result.TimedOut, result.NonVacuous = TimeoutExitCode, true, false
	case errors.Is(runCtx.Err(), context.Canceled):
		result.ExitCode, result.Interrupted, result.NonVacuous = InterruptedExitCode, true, false
	case runErr == nil:
		result.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			if result.ExitCode < 0 {
				result.ExitCode = 1
			}
			break
		}
		return Result{}, fmt.Errorf("await verification process: %w", runErr)
	}

	result.GoRunSelector, result.NonVacuous, result.ZeroMatch = goRunFacts(
		mode, request.Argv, stdout.observed.String(), stderr.observed.String(),
	)
	if result.TimedOut || result.Interrupted || result.ExitCode != 0 {
		result.NonVacuous = false
	}
	if result.ExitCode == 0 && result.GoRunSelector && !result.NonVacuous {
		result.ExitCode = ZeroMatchExitCode
	}
	result.Passed = result.ExitCode == 0 && result.NonVacuous &&
		!result.TimedOut && !result.Interrupted
	return result, nil
}

func canonicalDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("verify project directory is required")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("verify project directory must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("resolve verify project directory: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat verify project directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("verify project directory is not a directory")
	}
	return canonical, nil
}

func command(request Request) (string, []string, string, error) {
	switch {
	case len(request.Argv) != 0 && request.Shell != "":
		return "", nil, "", errors.New("verify command must choose argv or explicit shell semantics")
	case len(request.Argv) != 0:
		arguments := slices.Clone(request.Argv)
		for _, value := range arguments {
			if strings.IndexByte(value, 0) >= 0 {
				return "", nil, "", errors.New("verify argv contains NUL")
			}
		}
		if strings.TrimSpace(arguments[0]) == "" {
			return "", nil, "", errors.New("verify executable is empty")
		}
		return arguments[0], arguments[1:], "argv", nil
	case request.Shell != "":
		if strings.IndexByte(request.Shell, 0) >= 0 {
			return "", nil, "", errors.New("verify shell command contains NUL")
		}
		if strings.TrimSpace(request.Shell) == "" {
			return "", nil, "", errors.New("verify shell command is empty")
		}
		name, arguments := shellCommand(request.Shell)
		return name, arguments, "shell", nil
	default:
		return "", nil, "", errors.New("verify command is required")
	}
}

func commandDigest(mode string, argv []string, shell string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(mode))
	_, _ = digest.Write([]byte{0})
	if mode == "shell" {
		_, _ = digest.Write([]byte(shell))
	} else {
		for _, value := range argv {
			_, _ = digest.Write([]byte(value))
			_, _ = digest.Write([]byte{0})
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func goRunFacts(mode string, argv []string, outputs ...string) (supported, nonVacuous, zeroMatch bool) {
	if mode != "argv" || len(argv) < 2 ||
		strings.TrimSuffix(filepath.Base(argv[0]), ".exe") != "go" || argv[1] != "test" ||
		!hasRunSelector(argv[2:]) {
		return false, true, false
	}
	if !hasArgument(argv[2:], "-json") {
		nonVacuous, zeroMatch := goPlainFacts(outputs)
		return true, nonVacuous, zeroMatch
	}
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			var event struct {
				Action string `json:"Action"`
				Test   string `json:"Test"`
			}
			if json.Unmarshal([]byte(line), &event) == nil &&
				event.Action == "run" && event.Test != "" {
				return true, true, false
			}
		}
	}
	return true, false, true
}

// goPlainFacts derives selector facts from human-readable `go test` output. A
// package summary that is not marked as having run nothing proves at least one
// selected test executed; a zero-match claim is made only when every observed
// package summary reports that nothing ran.
func goPlainFacts(outputs []string) (nonVacuous, zeroMatch bool) {
	vacuousPackage := false
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimRight(line, " \t\r")
			switch {
			case strings.HasPrefix(line, "=== RUN"),
				strings.HasPrefix(line, "--- PASS:"), strings.HasPrefix(line, "--- FAIL:"):
				return true, false
			case strings.HasPrefix(line, "ok "), strings.HasPrefix(line, "ok\t"):
				if !strings.HasSuffix(line, "[no tests to run]") {
					return true, false
				}
				vacuousPackage = true
			case strings.HasPrefix(line, "? "), strings.HasPrefix(line, "?\t"):
				vacuousPackage = true
			}
		}
	}
	return false, vacuousPackage
}

func hasRunSelector(arguments []string) bool {
	for index, argument := range arguments {
		if strings.HasPrefix(argument, "-run=") && len(argument) > len("-run=") {
			return true
		}
		if argument == "-run" && index+1 < len(arguments) && arguments[index+1] != "" {
			return true
		}
	}
	return false
}

func hasArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted || strings.HasPrefix(argument, wanted+"=") {
			return true
		}
	}
	return false
}

var (
	credentialPattern  = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic)\s+|(?:api[_-]?key|token|password|secret)\s*[=:]\s*)[^\s]+`)
	urlPattern         = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s"'<>]+`)
	windowsPathPattern = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9._-])(?:[A-Z]:\\|\\\\)[^\s"'<>]+`)
	unixPathPattern    = regexp.MustCompile(`(^|[^A-Za-z0-9._-])/[^\s"'<>]+`)
	traversalPattern   = regexp.MustCompile(`(^|[^A-Za-z0-9._-])(?:\.\.[\\/])+(?:[^\s"'<>]*)`)
)

func redactExcerpt(value string) string {
	value = credentialPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = urlPattern.ReplaceAllString(value, "[URL]")
	value = windowsPathPattern.ReplaceAllString(value, `${1}[PATH]`)
	value = unixPathPattern.ReplaceAllString(value, `${1}[PATH]`)
	return traversalPattern.ReplaceAllString(value, `${1}[PATH]`)
}

type boundedDigest struct {
	limit     int
	captured  []byte
	observed  strings.Builder
	digestSum hash.Hash
	truncated bool
	total     int
}

func newBoundedDigest(limit int) *boundedDigest {
	return &boundedDigest{limit: limit, digestSum: sha256.New()}
}

func (writer *boundedDigest) Write(value []byte) (int, error) {
	_, _ = writer.digestSum.Write(value)
	writer.total += len(value)
	if writer.observed.Len() < MaximumOutputLimit {
		take := len(value)
		if remaining := MaximumOutputLimit - writer.observed.Len(); take > remaining {
			take = remaining
		}
		_, _ = writer.observed.Write(value[:take])
	}
	remaining := writer.limit - len(writer.captured)
	if remaining > 0 {
		take := len(value)
		if take > remaining {
			take = remaining
		}
		writer.captured = append(writer.captured, value[:take]...)
	}
	writer.truncated = writer.total > len(writer.captured)
	return len(value), nil
}

func (writer *boundedDigest) digest() string {
	return hex.EncodeToString(writer.digestSum.Sum(nil))
}

func (writer *boundedDigest) excerpt() string {
	if utf8.Valid(writer.captured) {
		return redactExcerpt(string(writer.captured))
	}
	return redactExcerpt(strings.ToValidUTF8(string(writer.captured), "\uFFFD"))
}
