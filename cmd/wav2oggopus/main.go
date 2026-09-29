package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"strings"

	opusgo "github.com/selawe/go-opus-codec"
	"github.com/selawe/go-opus-codec/internal/atomicfile"
	"github.com/selawe/go-opus-codec/opus"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wav2oggopus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		outPath     = fs.String("out", "out.opus", "output .opus file")
		cpuProfile  = fs.String("cpuprofile", "", "write CPU profile to file (disabled by default)")
		bitrate     = fs.Int("bitrate", 64000, "target bitrate in bits/sec")
		vbr         = fs.Bool("vbr", true, "enable variable bitrate")
		complexity  = fs.Int("complexity", 10, "encoder complexity (0-10)")
		application = fs.String("application", "audio", "opus application: audio|voip|lowdelay")
		frameMS     = fs.Int("frame-ms", 20, "frame duration in ms (5|10|20|40|60)")
		vendor      = fs.String("vendor", "opusgo", "OpusTags vendor string")
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
		fmt.Fprintf(stderr, "usage: wav2oggopus [flags] input.wav\n")
		fs.PrintDefaults()
		return 2
	}
	inPath := fs.Arg(0)

	app, err := parseApplication(*application)
	if err != nil {
		return fail(stderr, err)
	}
	if err := checkFrameMS(*frameMS); err != nil {
		return fail(stderr, err)
	}

	inF, err := os.Open(inPath)
	if err != nil {
		return fail(stderr, err)
	}
	defer inF.Close()

	// Written to a temp file (or a fresh file) and only made visible by Commit, so a
	// failure never leaves a partial output nor destroys an existing one.
	outF, err := atomicfile.Create(*outPath, inPath)
	if err != nil {
		return fail(stderr, err)
	}
	defer outF.Abort()

	// The library does the encoding: any WAV sample rate (resampled to 48 kHz when libopus
	// does not support it), pre-skip, RFC 7845 granule positions and lookahead flushing.
	err = opusgo.EncodeWAVToOggOpus(bufio.NewReaderSize(inF, 1<<20), outF, &opusgo.EncodeOptions{
		Bitrate:            *bitrate,
		CBR:                !*vbr,
		Complexity:         *complexity,
		ComplexityExplicit: *complexity >= 0, // a negative value keeps the encoder default of 10
		Application:        app,
		FrameSizeMS:        *frameMS,
		Vendor:             *vendor,
	})
	if err != nil {
		return fail(stderr, err)
	}
	if err := outF.Commit(); err != nil {
		return fail(stderr, err)
	}
	return 0
}

// checkFrameMS rejects frame durations libopus does not accept before any file is touched.
func checkFrameMS(ms int) error {
	switch ms {
	case 5, 10, 20, 40, 60:
		return nil
	}
	return fmt.Errorf("unsupported frame-ms %d (use 5|10|20|40|60)", ms)
}

func parseApplication(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "restricted-", "")
	s = strings.TrimPrefix(s, "app-")

	switch s {
	case "audio":
		return opus.ApplicationAudio, nil
	case "voip":
		return opus.ApplicationVoIP, nil
	case "lowdelay", "low-delay", "restricted-lowdelay":
		return opus.ApplicationRestrictedLowDelay, nil
	default:
		return 0, fmt.Errorf("unknown application: %q", s)
	}
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
