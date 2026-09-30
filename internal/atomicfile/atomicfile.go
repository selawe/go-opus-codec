// Package atomicfile writes output files without ever leaving a partial file behind
// or destroying an existing one when the write fails.
package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrSameFile is returned by Create when the destination is one of the input files.
var ErrSameFile = errors.New("output file is the same as an input file")

// File is an output file that is only made visible by Commit.
//
// Data always goes to a temporary file in the destination's directory, which replaces
// the destination on Commit. A crash, a kill or a failed write therefore never leaves a
// truncated file at the destination, and an existing file keeps its previous contents
// until the new one is complete. A symlink destination is resolved first, so the link
// survives and its target is the file that gets replaced. Special files (devices, pipes)
// such as /dev/stdout are written in place and never removed.
type File struct {
	*os.File

	dst    string
	remove string // temporary file to delete on Abort ("" for in-place special files)
	done   bool
}

// Create opens dst for writing. It fails with ErrSameFile if dst refers to any of the
// inputs, which would otherwise be replaced while still being read, and with a
// permission error if dst is an existing file that is not writable.
func Create(dst string, inputs ...string) (*File, error) {
	if resolved, rerr := filepath.EvalSymlinks(dst); rerr == nil {
		dst = resolved
	}
	dstInfo, err := os.Stat(dst)
	switch {
	case err == nil:
		for _, in := range inputs {
			if inInfo, ierr := os.Stat(in); ierr == nil && os.SameFile(dstInfo, inInfo) {
				return nil, fmt.Errorf("%w: %s", ErrSameFile, dst)
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}

	if err != nil { // new file: the usual 0666 &^ umask
		return createTemp(dst, 0o666, false)
	}
	if !dstInfo.Mode().IsRegular() {
		// Devices and pipes (for example /dev/stdout) are written in place: renaming over
		// them would replace the node itself, or the file the shell already redirected to.
		f, oerr := os.OpenFile(dst, os.O_WRONLY, 0)
		if oerr != nil {
			return nil, oerr
		}
		return &File{File: f, dst: dst}, nil
	}
	if dstInfo.Mode().Perm()&0o200 == 0 {
		// A rename would silently replace a read-only file.
		return nil, &os.PathError{Op: "create", Path: dst, Err: os.ErrPermission}
	}
	return createTemp(dst, dstInfo.Mode().Perm(), true)
}

func createTemp(dst string, perm os.FileMode, chmod bool) (*File, error) {
	dir, base := filepath.Dir(dst), filepath.Base(dst)
	openPerm := perm
	if chmod {
		openPerm = 0o600
	}
	for range 100 {
		var r [6]byte
		if _, err := rand.Read(r[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, "."+base+"."+hex.EncodeToString(r[:])+".tmp")
		tmp, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, openPerm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if chmod {
			if err := tmp.Chmod(perm); err != nil {
				_ = tmp.Close()
				_ = os.Remove(name)
				return nil, err
			}
		}
		return &File{File: tmp, dst: dst, remove: name}, nil
	}
	return nil, &os.PathError{Op: "create", Path: dst, Err: os.ErrExist}
}

// Commit flushes the file to disk and makes it visible at the destination.
// After a failed Commit the output is discarded as by Abort.
func (f *File) Commit() error {
	if f.done {
		return errors.New("atomicfile: already finished")
	}
	f.done = true

	var err error
	if f.remove != "" { // regular file: make sure the data reached the disk
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && f.remove != "" {
		err = os.Rename(f.remove, f.dst)
		if err == nil {
			syncDir(filepath.Dir(f.dst))
		}
	}
	if err != nil && f.remove != "" {
		_ = os.Remove(f.remove)
	}
	return err
}

// syncDir makes the rename durable. It is best effort: not every platform can fsync a directory.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// Abort discards the output. It is a no-op after Commit or a previous Abort, so it can
// be deferred right after Create.
func (f *File) Abort() {
	if f.done {
		return
	}
	f.done = true
	_ = f.Close()
	if f.remove != "" {
		_ = os.Remove(f.remove)
	}
}
