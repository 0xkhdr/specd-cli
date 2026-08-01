package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestRunExactArgvAndExplicitShell(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	literal := "literal; touch " + marker
	result, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"printf", "%s", literal}, Timeout: testTimeout,
	})
	if err != nil || !result.Passed || result.StdoutExcerpt != "literal; touch [PATH]" {
		t.Fatalf("argv result = %#v, %v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("argv metacharacters executed")
	}
	result, err = Run(context.Background(), Request{
		Dir: root, Shell: "touch marker", Timeout: testTimeout,
	})
	if err != nil || !result.Passed {
		t.Fatalf("shell result = %#v, %v", result, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("explicit shell semantics did not run")
	}
}

func TestRunParseCommandRequiresExplicitShell(t *testing.T) {
	request, err := ParseCommand(`go test ./internal/core -run 'TestRun|TestComplete'`)
	if err != nil || request.Shell != "" ||
		strings.Join(request.Argv, "\x00") != "go\x00test\x00./internal/core\x00-run\x00TestRun|TestComplete" {
		t.Fatalf("argv = %#v, %v", request, err)
	}
	request, err = ParseCommand(`shell: printf ok | grep ok`)
	if err != nil || request.Shell != "printf ok | grep ok" || len(request.Argv) != 0 {
		t.Fatalf("shell = %#v, %v", request, err)
	}
	for _, malformed := range []string{"", "go test | tee out", `"unterminated`, `shell: `} {
		if _, err := ParseCommand(malformed); err == nil {
			t.Fatalf("malformed command accepted: %q", malformed)
		}
	}
}

func TestRunCommandDigestMatchesPreflightIdentity(t *testing.T) {
	request := Request{
		Dir: t.TempDir(), Argv: []string{"printf", "%s", "ok"}, Timeout: testTimeout,
	}
	expected, err := CommandDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), request)
	if err != nil || result.CommandDigest != expected {
		t.Fatalf("command identity = %q, want %q: %v", result.CommandDigest, expected, err)
	}
	if _, err := CommandDigest(Request{Argv: []string{"printf"}, Shell: "printf bad"}); err == nil {
		t.Fatal("ambiguous command acquired an identity")
	}
}

func TestRunPassFailTimeoutAndInterruption(t *testing.T) {
	root := t.TempDir()
	pass, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "exit 0"}, Timeout: testTimeout,
	})
	if err != nil || !pass.Passed || pass.ExitCode != 0 || !pass.NonVacuous {
		t.Fatalf("pass = %#v, %v", pass, err)
	}
	fail, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "printf failed >&2; exit 7"}, Timeout: testTimeout,
	})
	if err != nil || fail.Passed || fail.ExitCode != 7 ||
		!strings.Contains(fail.StderrExcerpt, "failed") {
		t.Fatalf("fail = %#v, %v", fail, err)
	}
	timeout, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "sleep 5"}, Timeout: 20 * time.Millisecond,
	})
	if err != nil || timeout.Passed || !timeout.TimedOut ||
		timeout.ExitCode != TimeoutExitCode || timeout.NonVacuous {
		t.Fatalf("timeout = %#v, %v", timeout, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	interrupted, err := Run(ctx, Request{
		Dir: root, Argv: []string{"sh", "-c", "sleep 5"}, Timeout: testTimeout,
	})
	if err != nil || interrupted.Passed || !interrupted.Interrupted ||
		interrupted.ExitCode != InterruptedExitCode {
		t.Fatalf("interruption = %#v, %v", interrupted, err)
	}
}

func TestRunTimeoutTerminatesProcessTree(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "descendant-survived")
	result, err := Run(context.Background(), Request{
		Dir: root,
		Argv: []string{
			"sh", "-c", "(sleep .2; touch " + marker + ") & wait",
		},
		Timeout: 20 * time.Millisecond,
	})
	if err != nil || !result.TimedOut {
		t.Fatalf("timeout = %#v, %v", result, err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("timed-out descendant survived")
	}
}

func TestRunBoundedCaptureAndDigest(t *testing.T) {
	root := t.TempDir()
	result, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "head -c 8192 /dev/zero"},
		Timeout: testTimeout, OutputLimit: 64,
	})
	if err != nil || !result.Passed || !result.StdoutTruncated ||
		len(result.StdoutExcerpt) > 3*64 || len(result.StdoutDigest) != 64 {
		t.Fatalf("bounded result = %#v, %v", result, err)
	}
	again, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "head -c 8192 /dev/zero"},
		Timeout: testTimeout, OutputLimit: 64,
	})
	if err != nil || result.StdoutDigest != again.StdoutDigest ||
		result.CommandDigest != again.CommandDigest {
		t.Fatalf("digests changed: %#v / %#v / %v", result, again, err)
	}
}

func TestRunRedactsSensitiveExcerptsWithoutChangingDigest(t *testing.T) {
	root := t.TempDir()
	raw := "token=topsecret https://example.test/a " + filepath.Join(root, "private") + " ../escape"
	result, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"printf", "%s", raw}, Timeout: testTimeout,
	})
	if err != nil || !result.Passed {
		t.Fatalf("result = %#v, %v", result, err)
	}
	for _, sensitive := range []string{"topsecret", "https://", root, "../"} {
		if strings.Contains(result.StdoutExcerpt, sensitive) {
			t.Fatalf("excerpt leaked %q: %q", sensitive, result.StdoutExcerpt)
		}
	}
	sum := sha256.Sum256([]byte(raw))
	if result.StdoutDigest != hex.EncodeToString(sum[:]) {
		t.Fatal("output digest did not retain the raw observation")
	}
	punctuationAndUNC := `failed,/home/private; C:\Users\private \\server\share\private`
	result, err = Run(context.Background(), Request{
		Dir: root, Argv: []string{"printf", "%s", punctuationAndUNC}, Timeout: testTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"/home/private", `C:\Users\private`, `\\server\share\private`} {
		if strings.Contains(result.StdoutExcerpt, sensitive) {
			t.Fatalf("punctuation/UNC excerpt leaked %q: %q", sensitive, result.StdoutExcerpt)
		}
	}
}

func TestRunMalformedCommandNeverStarts(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	tests := []Request{
		{}, {Argv: []string{""}}, {Shell: "   "},
		{Argv: []string{"touch", marker}, Shell: "touch marker"},
		{Argv: []string{"bad\x00command"}}, {Shell: "bad\x00command"},
		{Argv: []string{"touch", marker}, Timeout: -1},
		{Argv: []string{"touch", marker}, Timeout: MaximumTimeout + 1},
		{Argv: []string{"touch", marker}, OutputLimit: MaximumOutputLimit + 1},
	}
	for _, request := range tests {
		request.Dir = root
		if _, err := Run(context.Background(), request); err == nil {
			t.Fatalf("malformed request started: %#v", request)
		}
	}
	for _, dir := range []string{"", "."} {
		if _, err := Run(context.Background(), Request{
			Dir: dir, Argv: []string{"touch", marker}, Timeout: testTimeout,
		}); err == nil {
			t.Fatalf("invalid project directory %q started", dir)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("malformed command created marker")
	}
}

func TestRunGoSelectorZeroMatchCannotPass(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/run\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testSource := "package run\nimport \"testing\"\nfunc TestSelected(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(root, "run_test.go"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"go", "test", "-json", ".", "-run", "^TestSelected$"},
		Timeout: testTimeout,
	})
	if err != nil || !selected.Passed || !selected.GoRunSelector || !selected.NonVacuous {
		t.Fatalf("selected = %#v, %v", selected, err)
	}
	empty, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"go", "test", "-json", ".", "-run=^TestMissing$"},
		Timeout: testTimeout,
	})
	if err != nil || empty.Passed || !empty.GoRunSelector || empty.NonVacuous ||
		!empty.ZeroMatch || empty.ExitCode != ZeroMatchExitCode {
		t.Fatalf("zero match = %#v, %v", empty, err)
	}
	humanOutput, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"go", "test", ".", "-run", "^TestSelected$"},
		Timeout: testTimeout,
	})
	if err != nil || !humanOutput.Passed || !humanOutput.GoRunSelector ||
		!humanOutput.NonVacuous || humanOutput.ZeroMatch {
		t.Fatalf("non-json selector = %#v, %v", humanOutput, err)
	}
	humanEmpty, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"go", "test", ".", "-run", "^TestMissing$"},
		Timeout: testTimeout,
	})
	if err != nil || humanEmpty.Passed || !humanEmpty.GoRunSelector ||
		humanEmpty.NonVacuous || !humanEmpty.ZeroMatch ||
		humanEmpty.ExitCode != ZeroMatchExitCode {
		t.Fatalf("non-json zero match = %#v, %v", humanEmpty, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	otherSource := "package other\nimport \"testing\"\nfunc TestOther(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(root, "other", "other_test.go"), []byte(otherSource), 0o600); err != nil {
		t.Fatal(err)
	}
	mixed, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"go", "test", "./...", "-run", "^TestSelected$"},
		Timeout: testTimeout,
	})
	if err != nil || !mixed.Passed || !mixed.NonVacuous || mixed.ZeroMatch {
		t.Fatalf("mixed selector = %#v, %v", mixed, err)
	}
}

func TestRunUnsupportedRunnerMakesNoZeroMatchClaim(t *testing.T) {
	root := t.TempDir()
	result, err := Run(context.Background(), Request{
		Dir: root, Argv: []string{"sh", "-c", "printf 'ok pkg [no tests to run]\\n'"},
		Timeout: testTimeout,
	})
	if err != nil || !result.Passed || result.GoRunSelector ||
		result.ZeroMatch || !result.NonVacuous {
		t.Fatalf("unsupported runner = %#v, %v", result, err)
	}
	shell, err := Run(context.Background(), Request{
		Dir: root, Shell: "printf 'ok pkg [no tests to run]\\n' # go test -run Missing",
		Timeout: testTimeout,
	})
	if err != nil || !shell.Passed || shell.GoRunSelector || shell.ZeroMatch {
		t.Fatalf("explicit shell = %#v, %v", shell, err)
	}
}
