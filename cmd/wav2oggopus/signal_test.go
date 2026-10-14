//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain lets the test binary double as the command: with the variable set it runs main()
// instead of the tests, so the signal test can start a real process without go build.
func TestMain(m *testing.M) {
	if os.Getenv("WAV2OGGOPUS_RUN_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func leftovers(t *testing.T, dir string, keep string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if e.Name() != keep {
			out = append(out, e.Name())
		}
	}
	return out
}

// Ctrl-C in the middle of an encode must exit 130 and leave neither the output nor its
// ".out.opus.<hex>.tmp" file behind.
func TestInterruptDiscardsTempFile(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes minutes of audio")
	}
	in := buildTestWAV(t, 48000, 1, 48000*240) // ~4 minutes: several seconds of encoding
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.opus")

	cmd := exec.Command(os.Args[0], "--out", out, in)
	cmd.Env = append(os.Environ(), "WAV2OGGOPUS_RUN_MAIN=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait until the temp file exists, so the signal lands mid-conversion.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if l := leftovers(t, dir, filepath.Base(in)); len(l) > 0 && strings.HasSuffix(l[0], ".tmp") {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("temp file never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 130 {
		t.Fatalf("exit = %v, want status 130", err)
	}
	if l := leftovers(t, dir, filepath.Base(in)); len(l) != 0 {
		t.Fatalf("files left behind after the interrupt: %v", l)
	}
}
