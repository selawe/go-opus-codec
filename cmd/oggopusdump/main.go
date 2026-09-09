package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/selawe/go-opus-codec/ogg"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("oggopusdump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		noCRC = fs.Bool("no-crc", false, "skip Ogg CRC verification")
		max   = fs.Int("max", 0, "stop after N audio packets (0 = all)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: oggopusdump [--no-crc] [--max N] file.ogg")
		return 2
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	defer f.Close()

	r, err := ogg.NewOpusReader(f)
	if err != nil {
		return fail(stderr, err)
	}
	r.SetVerifyCRC(!*noCRC)

	fmt.Fprintf(stdout, "OpusHead: version=%d channels=%d preSkip=%d inputRate=%d mapping=%d\n",
		r.Head.Version, r.Head.Channels, r.Head.PreSkip, r.Head.InputSampleRate, r.Head.ChannelMappingFamily)
	fmt.Fprintf(stdout, "OpusTags: vendor=%q comments=%d\n", r.Tags.Vendor, len(r.Tags.Comments))
	fmt.Fprintf(stdout, "Preskip samples: %d\n", r.Head.PreSkip)

	count := 0
	for {
		pkt, err := r.ReadAudioPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(stderr, err)
		}
		count++
		if pkt.GranuleValid {
			fmt.Fprintf(stdout, "pkt=%d bytes=%d granule=%d page=%d eos=%v\n", count, len(pkt.Data), pkt.GranulePos, pkt.PageSequence, pkt.EOS)
		} else {
			fmt.Fprintf(stdout, "pkt=%d bytes=%d granule=? (%d) page=%d eos=%v\n", count, len(pkt.Data), pkt.GranulePos, pkt.PageSequence, pkt.EOS)
		}
		if *max > 0 && count >= *max {
			break
		}
	}
	return 0
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
