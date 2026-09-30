package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/pprof"

	opusgo "github.com/selawe/go-opus-codec"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run returns instead of calling os.Exit so deferred cleanup (stopping and
// closing the CPU profile) also happens on error paths.
func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("oggopus2wav", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		out        = fs.String("out", "out.wav", "output wav file")
		cpuProfile = fs.String("cpuprofile", "", "write CPU profile to file (disabled by default)")
		maxBytes   = fs.Int64("max-bytes", 1<<30, "abort when the decoded PCM exceeds this many bytes (0 = only the 4 GiB WAV limit)")
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: oggopus2wav --out out.wav input.ogg")
		return 2
	}

	if *maxBytes < 0 {
		fmt.Fprintln(stderr, "error: --max-bytes must not be negative")
		return 2
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			return fail(stderr, err)
		}
		defer func() {
			_ = f.Close()
		}()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fail(stderr, err)
		}
		defer pprof.StopCPUProfile()
	}

	if err := opusgo.ConvertOggOpusFileToWAV(fs.Arg(0), *out, opusgo.WithMaxOutputBytes(*maxBytes)); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
