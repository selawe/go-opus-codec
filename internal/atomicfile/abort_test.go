package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAbortAllRemovesUnfinishedTempFilesOnly(t *testing.T) {
	dir := t.TempDir()

	done, err := Create(filepath.Join(dir, "done.bin"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, done, "kept")
	if err := done.Commit(); err != nil {
		t.Fatal(err)
	}

	a, err := Create(filepath.Join(dir, "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(filepath.Join(dir, "b.bin"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, a, "partial")
	write(t, b, "partial")

	AbortAll()

	if got := names(t, dir); len(got) != 1 || got[0] != "done.bin" {
		t.Fatalf("after AbortAll the directory holds %v, want only done.bin", got)
	}
	if got := read(t, filepath.Join(dir, "done.bin")); got != "kept" {
		t.Fatalf("committed file changed: %q", got)
	}
	// Both are finished now, so a late Abort or Commit is harmless / refused.
	a.Abort()
	if err := b.Commit(); err == nil {
		t.Fatal("Commit after AbortAll must fail")
	}
}

// A signal handler may call AbortAll while the main goroutine is committing. Whichever wins,
// the outcome must be consistent (either the finished file or nothing) and leave no temp file.
func TestCommitRacingAbortAll(t *testing.T) {
	dir := t.TempDir()
	for i := range 200 {
		dst := filepath.Join(dir, "out.bin")
		_ = os.Remove(dst)
		f, err := Create(dst)
		if err != nil {
			t.Fatal(err)
		}
		write(t, f, "payload")

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			AbortAll()
		}()
		cerr := f.Commit()
		wg.Wait()

		_, statErr := os.Stat(dst)
		switch {
		case cerr == nil && statErr != nil:
			t.Fatalf("iteration %d: Commit succeeded but the file is missing: %v", i, statErr)
		case cerr != nil && !errors.Is(statErr, os.ErrNotExist):
			t.Fatalf("iteration %d: Commit failed (%v) yet a file exists", i, cerr)
		}
		for _, n := range names(t, dir) {
			if n != "out.bin" {
				t.Fatalf("iteration %d: leftover file %q", i, n)
			}
		}
	}
}
