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

// A symlink destination keeps being a symlink: the data is written through it to the
// target instead of a rename replacing the link (the /dev/stdout case).
func TestSymlinkDestinationIsWrittenThrough(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.bin")
	link := filepath.Join(dir, "link.bin")
	if err := os.WriteFile(target, []byte("old contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f, err := Create(link)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "new")
	if err := f.Commit(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced (err=%v)", err)
	}
	if got := read(t, target); got != "new" {
		t.Fatalf("target = %q, want %q", got, "new")
	}
	if got := names(t, dir); len(got) != 2 {
		t.Fatalf("unexpected files: %v", got)
	}
}

// Nothing may appear at the destination until Commit, so a crash mid-write cannot
// leave a truncated output behind.
func TestNewFileIsInvisibleUntilCommit(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	f, err := Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Abort()
	write(t, f, "partial")
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination exists before Commit (err=%v)", err)
	}
	if err := f.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "partial" {
		t.Fatalf("content = %q", got)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("leftover files: %v", got)
	}
}

func TestReadOnlyDestinationIsRefused(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "ro.bin")
	if err := os.WriteFile(dst, []byte("keep"), 0o444); err != nil {
		t.Fatal(err)
	}
	if f, err := Create(dst); err == nil {
		f.Abort()
		if os.Geteuid() == 0 || runtime.GOOS == "windows" {
			t.Skip("permission bits not enforced here")
		}
		t.Fatal("expected a permission error")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v, want ErrPermission", err)
	}
	if got := read(t, dst); got != "keep" {
		t.Fatalf("content = %q", got)
	}
}

// A failed write through a symlink must leave the link's target intact.
func TestSymlinkTargetSurvivesAbort(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.bin")
	link := filepath.Join(dir, "link.bin")
	if err := os.WriteFile(target, []byte("victim"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f, err := Create(link)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "x")
	f.Abort()
	if got := read(t, target); got != "victim" {
		t.Fatalf("target = %q, want untouched", got)
	}
}
