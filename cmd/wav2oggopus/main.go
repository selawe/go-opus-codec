package main

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
	"github.com/selawe/go-opus-codec/wav"
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

	frameSize, err := frameSizeFromMS(*frameMS)
	if err != nil {
		return fail(stderr, err)
	}

	inF, err := os.Open(inPath)
	if err != nil {
		return fail(stderr, err)
	}
	defer inF.Close()

	wr, err := wav.NewReader(inF)
	if err != nil {
		return fail(stderr, err)
	}
	if wr.SampleRate() != 48000 {
		return fail(stderr, fmt.Errorf("only 48kHz WAV supported currently (got %d)", wr.SampleRate()))
	}
	if wr.Channels() < 1 || wr.Channels() > 2 {
		return fail(stderr, fmt.Errorf("only mono and stereo (1 or 2 channels) WAV files supported currently (got %d)", wr.Channels()))
	}

	enc, err := opus.NewEncoder(wr.SampleRate(), wr.Channels(), app)
	if err != nil {
		return fail(stderr, err)
	}
	defer enc.Close()

	if *bitrate > 0 {
		if err := enc.SetBitrate(*bitrate); err != nil {
			return fail(stderr, err)
		}
	}
	if err := enc.SetVBR(*vbr); err != nil {
		return fail(stderr, err)
	}
	if *complexity >= 0 {
		if err := enc.SetComplexity(*complexity); err != nil {
			return fail(stderr, err)
		}
	}

	lookahead, err := enc.Lookahead()
	if err != nil {
		return fail(stderr, err)
	}

	outF, err := os.Create(*outPath)
	if err != nil {
		return fail(stderr, err)
	}
	defer func() {
		_ = outF.Close()
	}()
	outBW := bufio.NewWriterSize(outF, 1<<20)
	defer func() {
		_ = outBW.Flush()
	}()

	serial := randomSerial()
	pw := ogg.NewPacketWriter(outBW, serial)

	head := ogg.OpusHead{
		Version:         1,
		Channels:        uint8(wr.Channels()),
		PreSkip:         uint16(lookahead),
		InputSampleRate: 48000,
		OutputGainQ8:    0,
		// ChannelMappingFamily=0 covers mono/stereo and lets decoders infer mapping.
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		return fail(stderr, err)
	}

	tags := ogg.OpusTags{
		Vendor:   *vendor,
		Comments: []string{"ENCODER=opusgo", "ENCODED=" + time.Now().UTC().Format(time.RFC3339)},
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		return fail(stderr, err)
	}

	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		return fail(stderr, err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		return fail(stderr, err)
	}

	frameSamples := frameSize * wr.Channels()
	pcm := make([]int16, frameSamples)
	packet := make([]byte, 4000)
	var inputSamplesPerCh uint64
	var encodedSamplesPerCh uint64
	var eofReached bool
	pending := make([]int16, 0, 2*frameSamples)
	readBuf := make([]int16, frameSamples)

	for {
		for len(pending) < frameSamples && !eofReached {
			n, rerr := wr.ReadInt16PCM(readBuf)
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				_ = os.Remove(*outPath)
				return fail(stderr, rerr)
			}
			if n > 0 {
				if n%wr.Channels() != 0 {
					_ = os.Remove(*outPath)
					return fail(stderr, fmt.Errorf("wav: sample count not multiple of channels"))
				}
				pending = append(pending, readBuf[:n]...)
				inputSamplesPerCh += uint64(n / wr.Channels())
			}
			if errors.Is(rerr, io.EOF) {
				eofReached = true
			}
		}

		if !eofReached {
			copy(pcm, pending[:frameSamples])
			pending = pending[frameSamples:]

			nBytes, err := enc.Encode(pcm, frameSize, packet)
			if err != nil {
				_ = os.Remove(*outPath)
				return fail(stderr, err)
			}
			encodedSamplesPerCh += uint64(frameSize)
			granule := uint64(head.PreSkip) + encodedSamplesPerCh

			if err := pw.WritePacket(packet[:nBytes], granule, false, false); err != nil {
				_ = os.Remove(*outPath)
				return fail(stderr, err)
			}
		} else {
			targetSamplesPerCh := ((inputSamplesPerCh + uint64(lookahead) + uint64(frameSize) - 1) / uint64(frameSize)) * uint64(frameSize)
			if targetSamplesPerCh == 0 {
				targetSamplesPerCh = uint64(frameSize)
			}
			eosGranule := uint64(head.PreSkip) + inputSamplesPerCh

			for encodedSamplesPerCh < targetSamplesPerCh {
				isLast := (encodedSamplesPerCh+uint64(frameSize) >= targetSamplesPerCh)

				toCopy := len(pending)
				if toCopy > frameSamples {
					toCopy = frameSamples
				}
				copy(pcm[:toCopy], pending[:toCopy])
				for i := toCopy; i < frameSamples; i++ {
					pcm[i] = 0
				}
				if toCopy > 0 {
					pending = pending[toCopy:]
				}

				nBytes, err := enc.Encode(pcm, frameSize, packet)
				if err != nil {
					_ = os.Remove(*outPath)
					return fail(stderr, err)
				}
				encodedSamplesPerCh += uint64(frameSize)

				granule := uint64(head.PreSkip) + encodedSamplesPerCh
				if isLast {
					granule = eosGranule
				}

				if err := pw.WritePacket(packet[:nBytes], granule, false, isLast); err != nil {
					_ = os.Remove(*outPath)
					return fail(stderr, err)
				}
			}
			break
		}
	}

	if err := pw.Flush(); err != nil {
		_ = os.Remove(*outPath)
		return fail(stderr, err)
	}
	return 0
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

func frameSizeFromMS(ms int) (int, error) {
	switch ms {
	case 5:
		return 240, nil
	case 10:
		return 480, nil
	case 20:
		return 960, nil
	case 40:
		return 1920, nil
	case 60:
		return 2880, nil
	default:
		return 0, fmt.Errorf("unsupported frame-ms %d (use 5|10|20|40|60)", ms)
	}
}

func randomSerial() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return binary.LittleEndian.Uint32(b[:])
	}
	// Fallback: time-based.
	return uint32(time.Now().UnixNano())
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
