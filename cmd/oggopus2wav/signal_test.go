//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	opusgo "github.com/selawe/go-opus-codec"
	"github.com/selawe/go-opus-codec/wav"
)

func TestMain(m *testing.M) {
	if os.Getenv("OGGOPUS2WAV_RUN_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// Ctrl-C in the middle of a decode must exit 130 and leave no temp file: the temp file is
// created inside the library, so this covers the registry rather than a handle the CLI holds.
func TestInterruptDiscardsTempFile(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes tens of minutes of audio")
	}
	// One minute of audio, encoded once and then chained 20 times (about 20 minutes to decode).
	ws := &bytes.Buffer{}
	{
		f, err := os.CreateTemp(t.TempDir(), "w-*.wav")
		if err != nil {
			t.Fatal(err)
		}
		ww, err := wav.NewWriter(f, 48000, 1)
		if err != nil {
			t.Fatal(err)
		}
		pcm := make([]int16, 48000)
		for i := range pcm {
			pcm[i] = int16(i % 2000)
		}
		for range 60 {
			if err := ww.WriteInt16PCM(pcm); err != nil {
				t.Fatal(err)
			}
		}
		if err := ww.Close(); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(f.Name())
		_ = f.Close()
		ws.Write(raw)
	}
	var one bytes.Buffer
	if err := opusgo.EncodeWAVToOggOpus(bytes.NewReader(ws.Bytes()), &one, &opusgo.EncodeOptions{Serial: 1}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.opus")
	if err := os.WriteFile(in, bytes.Repeat(one.Bytes(), 20), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.wav")

	cmd := exec.Command(os.Args[0], "--out", out, in)
	cmd.Env = append(os.Environ(), "OGGOPUS2WAV_RUN_MAIN=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ents, _ := os.ReadDir(dir)
		found := false
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".tmp") {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("temp file never appeared")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 130 {
		t.Fatalf("exit = %v, want status 130", err)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.Name() != "in.opus" {
			t.Fatalf("file left behind after the interrupt: %s", e.Name())
		}
	}
}
