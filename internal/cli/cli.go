// Package cli is the terminal entry point. It owns no operation metadata, no
// parsing rules, and no rendering: it resolves the invocation route, hands argv
// to the one dispatcher, and prints the one envelope that dispatcher's result
// projects. The envelope owns the exit code.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

// RouteVariable lets a host declare which surface it is. It is provenance, not
// proof: declaring a human terminal never raises any assurance label, and a host
// that lies only fools itself. Unset, the route is derived from stdin, and a
// non-terminal stdin is treated as agent-capable so the human gate fails closed.
const RouteVariable = "SPECD_ROUTE"

func Run(args []string, stdout, stderr io.Writer) int {
	if text, answered := intrinsic(args); answered {
		if _, err := io.WriteString(stdout, text); err != nil {
			return transportFailure(stderr, err)
		}
		return 0
	}
	route, err := selectRoute(os.Getenv(RouteVariable), os.Stdin)
	if err != nil {
		return transportFailure(stderr, err)
	}
	working, _ := os.Getwd()
	result, dispatchErr := cmd.Dispatch(context.Background(), cmd.Request{
		Args: args, Root: working, Actor: strings.TrimSpace(os.Getenv("USER")), Route: route,
	})
	outcome := cmd.Outcome{
		Operation: operationID(args), Root: hintedRoot(args, working),
		Value: result.Value, Err: dispatchErr, Exit: result.Exit,
	}
	// One envelope, one exit code, for both surfaces. A recorded-but-failing
	// verification and a fail-closed core refusal both take the exit class the
	// envelope assigns; the terminal never disagrees with JSON about it.
	if slices.Contains(args, "--json") {
		encoded, exit, err := cmd.RenderJSON(outcome)
		if err != nil {
			return transportFailure(stderr, firstNonNil(dispatchErr, err))
		}
		if _, err := stdout.Write(encoded); err != nil {
			return transportFailure(stderr, err)
		}
		return exit
	}
	envelope, err := cmd.Envelope(outcome)
	if err != nil {
		return transportFailure(stderr, firstNonNil(dispatchErr, err))
	}
	if _, err := io.WriteString(stdout, cmd.RenderText(envelope)); err != nil {
		return transportFailure(stderr, err)
	}
	return envelope.Exit.Code
}

// selectRoute reads the declared route, or derives it from stdin. An
// unrecognized declaration fails closed rather than defaulting to either side.
func selectRoute(declared string, stdin *os.File) (cmd.Route, error) {
	switch strings.TrimSpace(declared) {
	case string(cmd.RouteHumanTerminal):
		return cmd.RouteHumanTerminal, nil
	case string(cmd.RouteAgent):
		return cmd.RouteAgent, nil
	case "":
		// Derivation is one-directional: only a real terminal derives the human
		// route. Anything else — a pipe, a file, /dev/null, an unprobed
		// platform — derives agent, so an undetected human is inconvenienced
		// and an undetected agent is never let through.
		if isTerminal(stdin) {
			return cmd.RouteHumanTerminal, nil
		}
		return cmd.RouteAgent, nil
	}
	return "", failure.New("route_unknown", "", "",
		fmt.Sprintf("%s=%q is not a declared route", RouteVariable, declared),
		fmt.Sprintf("set %s to %s or %s", RouteVariable,
			cmd.RouteHumanTerminal, cmd.RouteAgent))
}

// intrinsic answers the two questions a caller has before it has a root, a
// change, or an operation to name: what can this binary do, and which build is
// it. They are argv conveniences rather than operations — they resolve no root,
// touch no managed byte, and are answered from the registry rather than added
// to it, so the operation registry keeps describing exactly the surface that
// acts on a project. A registered id always wins, so registering `help` or
// `version` later shadows this rather than being shadowed by it.
func intrinsic(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	if _, registered := core.OperationByID(args[0]); registered {
		return "", false
	}
	switch args[0] {
	case "--help", "-h", "help":
		return helpText(), true
	case "--version", "-v", "version":
		return versionText(), true
	}
	return "", false
}

// helpText renders the operation palette from the registry. It restates no
// flag: per-operation usage comes from Operation.Usage, and the full reference
// is the generated operations document.
func helpText() string {
	var out strings.Builder
	out.WriteString("specd — a spec-driven coding harness. The agent reasons; the harness enforces.\n\n")
	out.WriteString("usage: specd <operation> [arguments] [flags]\n\n")
	width := 0
	for _, operation := range core.Operations() {
		if operation.Executable && len(operation.ID) > width {
			width = len(operation.ID)
		}
	}
	for _, operation := range core.Operations() {
		if !operation.Executable {
			continue
		}
		fmt.Fprintf(&out, "  %-*s  %s\n", width, operation.ID, operation.Summary)
	}
	out.WriteString("\nglobal flags:\n")
	out.WriteString("  --root <root>  Select the project root holding .specd.\n")
	out.WriteString("  --json         Emit the machine-readable result document.\n")
	out.WriteString("\n  --version      Report the build.\n")
	out.WriteString("  --help         Show this palette.\n")
	out.WriteString("\nEvery flag, exit code, and allowed lifecycle: docs/operations.md\n")
	return out.String()
}

// stampedVersion is empty in every build except a release artifact, where the
// release workflow sets it with -ldflags -X from the pushed tag. It exists
// because a cross-compiled `go build` is not a module download: the module
// system never resolves a version for it, so without this the published binary
// reports `devel` and a user cannot tell which release they are holding. It
// cannot disagree with the tag because the tag is the only thing that writes
// it, and it is deliberately not a constant a committed source edit can set.
var stampedVersion string

// versionText reports what the build system stamped. There is no compiled-in
// version constant to disagree with the tag: a released build reports its
// module version, and a `go build` from a checkout reports the revision it was
// built from, so a bug report can always name one build.
func versionText() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "specd (unknown build)\n"
	}
	version, tagged := info.Main.Version, true
	if version == "" || version == "(devel)" {
		version, tagged = "devel", false
	}
	if stampedVersion != "" {
		version, tagged = stampedVersion, true
	}
	out := "specd " + version
	// A module version resolved by the module system already carries the
	// revision; only an untagged local build has to be told which commit it is.
	if !tagged {
		revision, modified := "", false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
		if revision != "" {
			out += " (" + revision
			if modified {
				out += "-dirty"
			}
			out += ")"
		}
	}
	return out + " " + info.GoVersion + "\n"
}

// operationID names the operation the envelope reports. An unregistered verb has
// no envelope to report it in, so it stays empty and the refusal is a transport
// failure instead of a document that names an operation nobody registered.
func operationID(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if _, known := core.OperationByID(args[0]); !known {
		return ""
	}
	return args[0]
}

// hintedRoot is presentation only: it tells the envelope which root the user
// named so a refusal can still show it. Dispatch remains the authority on root
// selection and on refusing an ambiguous one.
func hintedRoot(args []string, working string) string {
	selected := ""
	for index, argument := range args {
		if argument == "--root" && index+1 < len(args) {
			selected = args[index+1]
		}
	}
	if selected == "" && len(args) > 1 && args[0] == "init" && !strings.HasPrefix(args[1], "-") {
		selected = args[1]
	}
	if selected == "" {
		selected = working
	}
	canonical, err := corepath.ResolveRoot(selected, "")
	if err != nil {
		return ""
	}
	return canonical
}

// transportFailure is the only path to stderr: the envelope could not be built
// or written, so there is no document to print on stdout.
func transportFailure(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	return 2
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
