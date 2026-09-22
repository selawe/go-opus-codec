package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"

	opusgo "github.com/selawe/go-opus-codec"
)

func main() {
	var (
		out        = flag.String("out", "out.wav", "output wav file")
		cpuProfile = flag.String("cpuprofile", "", "write CPU profile to file (disabled by default)")
	)
	flag.Parse()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fatal(err)
		}
		defer func() {
			_ = f.Close()
		}()
		if err := pprof.StartCPUProfile(f); err != nil {
			fatal(err)
		}
		defer pprof.StopCPUProfile()
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: oggopus2wav --out out.wav input.ogg")
		os.Exit(2)
	}

	if err := opusgo.ConvertOggOpusFileToWAV(flag.Arg(0), *out); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

