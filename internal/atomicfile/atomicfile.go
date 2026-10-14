// Package atomicfile writes output files without ever leaving a partial file behind
// or destroying an existing one when the write fails.
package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
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

	mu   sync.Mutex // guards done, and serialises Commit/Abort against AbortAll from a signal goroutine
	done bool
}

// active tracks every File that has been created and neither committed nor aborted, so a
// signal handler can discard their temporary files.
var active = struct {
	sync.Mutex
	files map[*File]struct{}
}{files: map[*File]struct{}{}}

func register(f *File) *File {
	active.Lock()
	active.files[f] = struct{}{}
	active.Unlock()
	return f
}

func unregister(f *File) {
	active.Lock()
	delete(active.files, f)
	active.Unlock()
}

// AbortAll discards every unfinished File, removing their temporary files. It is what an
// interrupt handler calls, since the deferred Abort of a killed process never runs.
func AbortAll() {
	active.Lock()
	files := make([]*File, 0, len(active.files))
	for f := range active.files {
		files = append(files, f)
	}
	active.Unlock() // Abort re-takes the registry lock through unregister
	for _, f := range files {
		f.Abort()
	}
}

// AbortOnInterrupt makes SIGINT and SIGTERM discard all unfinished Files and then exit the
// process with status 130 or 143 (128 plus the signal number), instead of leaving
// ".name.<hex>.tmp" files behind. It is meant for the command line tools; call the returned
// function to restore the default behaviour.
func AbortOnInterrupt() (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-ch:
			AbortAll()
			if sig == syscall.SIGTERM {
				os.Exit(143)
			}
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
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
		return register(&File{File: f, dst: dst}), nil
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
		return register(&File{File: tmp, dst: dst, remove: name}), nil
	}
	return nil, &os.PathError{Op: "create", Path: dst, Err: os.ErrExist}
}

// Commit flushes the file to disk and makes it visible at the destination.
// After a failed Commit the output is discarded as by Abort.
func (f *File) Commit() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return errors.New("atomicfile: already finished")
	}
	f.done = true
	unregister(f)

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
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return
	}
	f.done = true
	unregister(f)
	_ = f.Close()
	if f.remove != "" {
		_ = os.Remove(f.remove)
	}
}
