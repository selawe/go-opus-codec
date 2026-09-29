// Package atomicfile writes output files without ever leaving a partial file behind
// or destroying an existing one when the write fails.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrSameFile is returned by Create when the destination is one of the input files.
var ErrSameFile = errors.New("output file is the same as an input file")

// File is an output file that is only made visible by Commit.
//
// Where the destination is a new path the file is created there directly and removed
// again by Abort. Where it already exists as a regular file, data goes to a temporary
// file in the same directory that replaces the destination on Commit, so a failed
// write leaves the previous contents untouched. Special files (devices, pipes) and symlinks
// such as /dev/stdout are written in place and never removed.
type File struct {
	*os.File

	dst    string
	remove string // path to delete on Abort ("" for in-place special files)
	rename bool   // Commit must rename the temporary file over dst
	done   bool
}

// Create opens dst for writing. It fails with ErrSameFile if dst refers to any of the
// inputs, which would otherwise be truncated while still being read.
func Create(dst string, inputs ...string) (*File, error) {
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

	if err == nil {
		// Devices, pipes and symlinks (for example /dev/stdout, a link to /proc/self/fd/1)
		// are written in place: renaming over them would replace the link itself, or the
		// file the shell already redirected to, and the data would be lost.
		if linfo, lerr := os.Lstat(dst); !dstInfo.Mode().IsRegular() || (lerr == nil && linfo.Mode()&os.ModeSymlink != 0) {
			flags := os.O_WRONLY
			if dstInfo.Mode().IsRegular() { // only a real file is truncated, never a device
				flags |= os.O_TRUNC
			}
			f, oerr := os.OpenFile(dst, flags, 0)
			if oerr != nil {
				return nil, oerr
			}
			return &File{File: f, dst: dst}, nil
		}
		return createReplacement(dst, dstInfo.Mode().Perm())
	}

	// New file: O_EXCL guarantees we never clobber something created since the Stat.
	f, oerr := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(oerr, os.ErrExist) {
		return createReplacement(dst, 0o666)
	}
	if oerr != nil {
		return nil, oerr
	}
	return &File{File: f, dst: dst, remove: dst}, nil
}

func createReplacement(dst string, perm os.FileMode) (*File, error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return nil, err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	return &File{File: tmp, dst: dst, remove: tmp.Name(), rename: true}, nil
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
	if err == nil && f.rename {
		err = os.Rename(f.remove, f.dst)
	}
	if err != nil && f.remove != "" {
		_ = os.Remove(f.remove)
	}
	return err
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
