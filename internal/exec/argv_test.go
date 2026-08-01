package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testTimeout is the budget for a command these tests expect to finish, not a
// deadline any of them measure. It has to cover spawning a process on a loaded
// CI runner under -race, which on Windows exceeded a second and failed the
// suite. Cases that assert timeout behavior set their own short deadline.
const testTimeout = 30 * time.Second

func TestArgvKeepsMetacharactersLiteral(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel")
	injections := []string{
		"a; touch " + sentinel,
		"a && touch " + sentinel,
		"a | touch " + sentinel,
		"$(touch " + sentinel + ")",
		"`touch " + sentinel + "`",
		"a\ntouch " + sentinel,
		"*",
	}
	for _, injection := range injections {
		result, err := Run(context.Background(), Request{
			Dir: dir, Argv: []string{"printf", "%s", injection}, Timeout: testTimeout,
		})
		if err != nil {
			t.Fatalf("run %q: %v", injection, err)
		}
		if result.Stdout != injection {
			t.Fatalf("argument was interpreted: got %q want %q", result.Stdout, injection)
		}
		if _, err := os.Stat(sentinel); err == nil {
			t.Fatalf("injection %q started a sibling command", injection)
		}
	}
}

func TestArgvFailsClosed(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]Request{
		"empty argv":        {Dir: dir},
		"empty executable":  {Dir: dir, Argv: []string{"  "}},
		"nul argument":      {Dir: dir, Argv: []string{"printf", "a\x00b"}},
		"relative dir":      {Dir: "relative", Argv: []string{"printf", "a"}},
		"missing dir":       {Argv: []string{"printf", "a"}},
		"negative timeout":  {Dir: dir, Argv: []string{"printf", "a"}, Timeout: -1},
		"excessive timeout": {Dir: dir, Argv: []string{"printf", "a"}, Timeout: MaximumTimeout + 1},
		"excessive output":  {Dir: dir, Argv: []string{"printf", "a"}, OutputLimit: MaximumOutputLimit + 1},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Run(context.Background(), request); err == nil {
				t.Fatal("expected refusal")
			} else if !strings.Contains(err.Error(), "next: ") {
				t.Fatalf("refusal names no next action: %v", err)
			}
		})
	}
}

func TestArgvBoundsOutputAndTime(t *testing.T) {
	dir := t.TempDir()
	result, err := Run(context.Background(), Request{
		Dir: dir, Argv: []string{"sh", "-c", "head -c 8192 /dev/zero | tr '\\0' 'x'"},
		Timeout: testTimeout, OutputLimit: 128,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Stdout) != 128 || !result.StdoutTruncated {
		t.Fatalf("stdout %d bytes truncated=%t", len(result.Stdout), result.StdoutTruncated)
	}
	timed, err := Run(context.Background(), Request{
		Dir: dir, Argv: []string{"sleep", "5"}, Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !timed.TimedOut || timed.ExitCode != TimeoutExitCode {
		t.Fatalf("timeout not reported: %+v", timed)
	}
}

func TestArgvReportsExitCode(t *testing.T) {
	result, err := Run(context.Background(), Request{
		Dir: t.TempDir(), Argv: []string{"sh", "-c", "printf failed >&2; exit 7"},
		Timeout: testTimeout,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ExitCode != 7 || result.Stderr != "failed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
