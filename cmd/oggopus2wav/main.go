package main

import (
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
	)
	if err := fs.Parse(args); err != nil {
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

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: oggopus2wav --out out.wav input.ogg")
		return 2
	}

	if err := opusgo.ConvertOggOpusFileToWAV(fs.Arg(0), *out); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
