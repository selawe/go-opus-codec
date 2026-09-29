package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, f *File, s string) {
	t.Helper()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestNewFileCommitAndAbort(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")

	f, err := Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "hello")
	f.Abort()
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("Abort left files behind: %v", got)
	}

	f, err = Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Abort() // must be a no-op after Commit
	write(t, f, "hello")
	if err := f.Commit(); err != nil {
		t.Fatal(err)
	}
	f.Abort()
	if got := read(t, dst); got != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func TestExistingFileSurvivesAbortAndIsReplacedOnCommit(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dst, []byte("precious"), 0o640); err != nil {
		t.Fatal(err)
	}

	f, err := Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "partial")
	if got := read(t, dst); got != "precious" {
		t.Fatalf("destination changed before Commit: %q", got)
	}
	f.Abort()
	if got := read(t, dst); got != "precious" {
		t.Fatalf("Abort destroyed the existing file: %q", got)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("temp file left behind: %v", got)
	}

	f, err = Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "new")
	if err := f.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "new" {
		t.Fatalf("content = %q", got)
	}
	// Windows has no POSIX permission bits to preserve.
	if fi, _ := os.Stat(dst); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 preserved", fi.Mode().Perm())
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("temp file left behind: %v", got)
	}
}

func TestRefusesToOverwriteInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.wav")
	if err := os.WriteFile(in, []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Same file through a different spelling and through a hard link.
	link := filepath.Join(dir, "link.wav")
	if err := os.Link(in, link); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	for _, dst := range []string{in, filepath.Join(dir, ".", "in.wav"), link} {
		if _, err := Create(dst, in); !errors.Is(err, ErrSameFile) {
			t.Errorf("Create(%q) err = %v, want ErrSameFile", dst, err)
		}
	}
	if got := read(t, in); got != "input" {
		t.Fatalf("input modified: %q", got)
	}
}

func TestSpecialFilesAreWrittenInPlaceAndNeverRemoved(t *testing.T) {
	if _, err := os.Stat(os.DevNull); err != nil {
		t.Skip("no /dev/null")
	}
	f, err := Create(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "x")
	f.Abort()
	if _, err := os.Stat(os.DevNull); err != nil {
		t.Fatalf("Abort removed %s: %v", os.DevNull, err)
	}
}
