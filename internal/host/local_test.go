package host

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/exec"
	"github.com/0xkhdr/specd-cli/internal/generate"
)

func TestLocalAdapter(t *testing.T) {
	t.Run("capabilities are independent and never inferred", func(t *testing.T) {
		local := LocalCapabilities()
		if !local.CanInstallSkill {
			t.Fatal("the local host can write a guidance file")
		}
		if local.CanInstallCommandWrapper || local.CanHideHumanOperations ||
			local.CanEnforceToolPathRestrictions || local.CanAttestActorClass {
			t.Fatalf("local host over-declares: %#v", local)
		}
		// Existing is not enforcing: an adapter with no declared capability is
		// still only advisory, and so is the shipped local one.
		adapter, err := Local(t.TempDir(), Capabilities{})
		if err != nil {
			t.Fatal(err)
		}
		if adapter.Assurance() != AssuranceAdvisory || local.Assurance() != AssuranceAdvisory {
			t.Fatalf("assurance = %q/%q, want %q",
				adapter.Assurance(), local.Assurance(), AssuranceAdvisory)
		}
		if AssuranceAdvisory != core.AttemptAssurance {
			t.Fatalf("adapter assurance %q disagrees with the harness label %q",
				AssuranceAdvisory, core.AttemptAssurance)
		}
		// Every one of the three enforcement facts is load-bearing.
		full := Capabilities{
			CanHideHumanOperations:         true,
			CanEnforceToolPathRestrictions: true,
			CanAttestActorClass:            true,
		}
		if full.Assurance() != AssuranceHostEnforced {
			t.Fatalf("full assurance = %q", full.Assurance())
		}
		for _, lowered := range []Capabilities{
			{CanEnforceToolPathRestrictions: true, CanAttestActorClass: true},
			{CanHideHumanOperations: true, CanAttestActorClass: true},
			{CanHideHumanOperations: true, CanEnforceToolPathRestrictions: true},
		} {
			if lowered.Assurance() != AssuranceAdvisory {
				t.Fatalf("%#v claimed %q", lowered, lowered.Assurance())
			}
		}
	})

	t.Run("callable ids match the agent registry projection", func(t *testing.T) {
		adapter, err := Local(t.TempDir(), LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		for _, operation := range core.Operations() {
			if operation.AgentVisible && operation.Executable {
				want = append(want, operation.ID)
				continue
			}
			if slices.Contains(adapter.Callable(), operation.ID) {
				t.Fatalf("adapter exposes non-agent operation %q", operation.ID)
			}
		}
		if !slices.Equal(adapter.Callable(), want) {
			t.Fatalf("callable = %v, want %v", adapter.Callable(), want)
		}
		if slices.Contains(adapter.Callable(), "approve") {
			t.Fatal("the human gate is callable")
		}
	})

	t.Run("supported installation writes only managed surfaces", func(t *testing.T) {
		root := t.TempDir()
		user := filepath.Join(root, "README.md")
		if err := os.WriteFile(user, []byte("mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		adapter, err := Local(root, LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		written, err := adapter.Install(SurfaceSkill)
		if err != nil {
			t.Fatal(err)
		}
		if len(written) != 1 || filepath.Base(written[0]) != generate.GuidanceFile {
			t.Fatalf("installed %v", written)
		}
		raw, err := os.ReadFile(written[0])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "specd next") {
			t.Fatalf("installed guidance is not the generated product: %q", raw)
		}
		if mine, err := os.ReadFile(user); err != nil || string(mine) != "mine\n" {
			t.Fatalf("install touched a user file: %q (%v)", mine, err)
		}
	})

	t.Run("absent capability refuses without a wider fallback", func(t *testing.T) {
		root := t.TempDir()
		adapter, err := Local(root, LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.Install(SurfaceCommandWrapper); !failure.IsCode(err, "adapter_capability_absent") {
			t.Fatalf("wrapper install = %v", err)
		}
		if _, err := adapter.Install("editor"); !failure.IsCode(err, "adapter_surface_unknown") {
			t.Fatalf("unknown surface = %v", err)
		}
		blind, err := Local(root, Capabilities{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := blind.Install(SurfaceSkill); !failure.IsCode(err, "adapter_capability_absent") {
			t.Fatalf("skill install without capability = %v", err)
		}
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
			t.Fatalf("refused installs wrote %v (%v)", entries, err)
		}
	})

	t.Run("unsafe destinations refuse", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		if err := os.WriteFile(outside, []byte("user\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, generate.GuidanceFile)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		adapter, err := Local(root, LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.Install(SurfaceSkill); !failure.IsCode(err, "generation_path_symlink") {
			t.Fatalf("symlink install = %v", err)
		}
		if raw, err := os.ReadFile(outside); err != nil || string(raw) != "user\n" {
			t.Fatalf("symlink escape wrote %q (%v)", raw, err)
		}
		if _, err := Local(filepath.Join(root, "..", "absent-project"), LocalCapabilities()); err == nil {
			t.Fatal("traversal root accepted")
		}
	})

	t.Run("invocation is bounded literal argv", func(t *testing.T) {
		root := t.TempDir()
		adapter, err := Local(root, LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		metacharacters := "safe-create; rm -rf / && echo $(whoami)"
		request, err := adapter.Invoke("next", metacharacters)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"specd", "next", metacharacters, "--root", adapter.Root()}
		if !slices.Equal(request.Argv, want) || request.Dir != adapter.Root() {
			t.Fatalf("request = %#v, want %v", request, want)
		}
		// The same argv reaches a real process without a shell in between.
		command, err := exec.Command(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(command.Args, want) {
			t.Fatalf("process args = %v", command.Args)
		}
		if _, err := adapter.Invoke("next", " "); !failure.IsCode(err, "adapter_argument_unsafe") {
			t.Fatalf("empty argument = %v", err)
		}
		if _, err := adapter.Invoke("next", "a\x00b"); !failure.IsCode(err, "adapter_argument_unsafe") {
			t.Fatalf("NUL argument = %v", err)
		}
	})

	t.Run("human-only and unknown operations have no adapter form", func(t *testing.T) {
		adapter, err := Local(t.TempDir(), LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		for operation, code := range map[string]string{
			"approve": "human_approval_required",
			"sync":    "human_operation_required",
			"resume":  "adapter_operation_unknown",
		} {
			_, err := adapter.Invoke(operation, "safe-create")
			if !failure.IsCode(err, code) {
				t.Fatalf("invoke %s = %v, want %s", operation, err, code)
			}
			var refusal *failure.Refusal
			if failure.IsCode(err, code) && err.(*failure.Refusal).Next == "" {
				t.Fatalf("invoke %s named no next action: %#v", operation, refusal)
			}
		}
	})
}
