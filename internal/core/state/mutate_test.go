package state

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestMutateConcurrentRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data, _ := Encode(Initial("cache-ttl", "create-1"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := Mutate(path, "cache-ttl", 1, func(s *State) error {
				s.LastTransition = "transition-2"
				return nil
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var success, stale int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case failure.IsCode(err, "stale_revision"):
			stale++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("success=%d stale=%d", success, stale)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Decode(got, "cache-ttl")
	if err != nil || s.Revision != 2 {
		t.Fatalf("state=%+v err=%v", s, err)
	}
}

// Mutate must take the one change lock <change>/.lock that every other state
// writer takes, never a second state.json.lock name.
func TestMutateLocksChangeLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	data, _ := Encode(Initial("cache-ttl", "create-1"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Mutate(path, "cache-ttl", 1, func(*State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lock")); err != nil {
		t.Fatalf("change lock missing: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second lock name used: %v", err)
	}
}

func TestMutateFailureDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	before, _ := Encode(Initial("cache-ttl", "create-1"))
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	if _, err := Mutate(path, "cache-ttl", 1, func(*State) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("failed mutation changed state")
	}
}
