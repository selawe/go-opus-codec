package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/pprof"

	"github.com/kazzmir/opus-go/ogg"
	"github.com/kazzmir/opus-go/opus"
	"github.com/kazzmir/opus-go/wav"
)

func main() {
	var (
		out        = flag.String("out", "out.wav", "output wav file")
		cpuProfile = flag.String("cpuprofile", "cpu.pprof", "write CPU profile to file (set to empty to disable)")
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

	in, err := os.Open(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	defer in.Close()

	r, err := ogg.NewOpusReader(in)
	if err != nil {
		fatal(err)
	}

	dec, err := opus.NewDecoderFromHead(r.Head)
	if err != nil {
		fatal(err)
	}
	defer dec.Close()

	outf, err := os.Create(*out)
	if err != nil {
		fatal(err)
	}
	defer outf.Close()

	ww, err := wav.NewWriter(outf, 48000, int(r.Head.Channels))
	if err != nil {
		fatal(err)
	}
	defer ww.Close()

	// Maximum Opus frame size at 48kHz is 120ms = 5760 samples/channel.
	maxFrame := 5760
	channels := int(r.Head.Channels)
	pcm := make([]int16, maxFrame*channels)

	preSkipRemaining := int(r.Head.PreSkip)
	totalSamplesDecoded := uint64(0)

	for {
		pkt, err := r.ReadAudioPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal(err)
		}

		n, err := dec.Decode(pkt.Data, pcm, maxFrame, false)
		if err != nil {
			fatal(err)
		}

		totalSamplesDecoded += uint64(n)

		// n is samples per channel.
		frames := pcm[:n*channels]

		// Apply OpusHead pre-skip in samples per channel.
		if preSkipRemaining > 0 {
			skip := preSkipRemaining
			if skip > n {
				skip = n
			}
			preSkipRemaining -= skip

			// Drop 'skip' samples per channel from interleaved PCM.
			drop := skip * channels
			if drop >= len(frames) {
				continue
			}
			frames = frames[drop:]
		}

		// RFC 7845 Section 4: If packet has a valid granule position (especially at EOS),
		// trim any trailing excess samples beyond the granule position.
		if pkt.GranuleValid && totalSamplesDecoded > pkt.GranulePos {
			excess := totalSamplesDecoded - pkt.GranulePos
			excessSamples := int(excess) * channels
			if excessSamples < len(frames) {
				frames = frames[:len(frames)-excessSamples]
			} else {
				frames = nil
			}
		}

		if len(frames) > 0 {
			if err := ww.WriteInt16PCM(frames); err != nil {
				fatal(err)
			}
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
