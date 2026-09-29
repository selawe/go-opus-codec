package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
	"github.com/selawe/go-opus-codec/wav"
)

// buildTestWAV synthesizes a minimal 16-bit PCM WAV file at the given
// sample rate and channel count and returns its path plus the number of
// interleaved sample frames written.
func buildTestWAV(t *testing.T, sampleRate, channels int, frames int) (path string) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "in-*.wav")
	if err != nil {
		t.Fatalf("create temp wav: %v", err)
	}
	defer f.Close()

	ww, err := wav.NewWriter(f, sampleRate, channels)
	if err != nil {
		t.Fatalf("wav.NewWriter: %v", err)
	}

	pcm := make([]int16, frames*channels)
	for i := range pcm {
		t := float64(i) / float64(sampleRate)
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*220*t))
	}
	if err := ww.WriteInt16PCM(pcm); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}
	if err := ww.Close(); err != nil {
		t.Fatalf("wav Close: %v", err)
	}
	return f.Name()
}

func TestWav2OggOpus_SuccessRoundTrip(t *testing.T) {
	const sampleRate = 48000
	const channels = 1
	const frames = 48000 // 1 second

	wavPath := buildTestWAV(t, sampleRate, channels, frames)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	outF, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer outF.Close()

	r, err := ogg.NewOpusReader(outF)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}
	if r.Head.Channels != channels {
		t.Fatalf("expected %d channels, got %d", channels, r.Head.Channels)
	}
	if r.Head.InputSampleRate != sampleRate {
		t.Fatalf("expected input sample rate %d, got %d", sampleRate, r.Head.InputSampleRate)
	}
	if r.Tags.Vendor != "opusgo" {
		t.Fatalf("expected default vendor %q, got %q", "opusgo", r.Tags.Vendor)
	}

	dec, err := opus.NewDecoderFromHead(r.Head)
	if err != nil {
		t.Fatalf("NewDecoderFromHead: %v", err)
	}
	defer dec.Close()

	pcmBuf := make([]int16, 5760*channels)
	var totalDecoded int
	for {
		pkt, err := r.ReadAudioPacket()
		if err != nil {
			break
		}
		n, err := dec.Decode(pkt.Data, pcmBuf, 5760, false)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		totalDecoded += n
	}

	// Opus is lossy/frame-quantized (and pre-skip/EOS trimming apply), so
	// assert the decoded length is close to the original rather than exact.
	const frameSize = 960
	if diff := totalDecoded - frames; diff > frameSize || diff < -frameSize {
		t.Fatalf("expected decoded sample count near %d, got %d (diff=%d)", frames, totalDecoded, diff)
	}
}

func TestWav2OggOpus_VendorFlag(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", "--vendor", "test-vendor", wavPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}

	outF, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer outF.Close()

	r, err := ogg.NewOpusReader(outF)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}
	if r.Tags.Vendor != "test-vendor" {
		t.Fatalf("expected vendor %q, got %q", "test-vendor", r.Tags.Vendor)
	}
}

func TestWav2OggOpus_BitrateAndComplexityFlags(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 2, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", "--bitrate", "32000", "--complexity", "5", wavPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
}

func TestWav2OggOpus_UnknownApplication(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", "--application", "bogus", wavPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown application") {
		t.Fatalf("expected stderr to mention unknown application, got: %s", stderr.String())
	}
}

func TestWav2OggOpus_UnknownFrameMS(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", "--frame-ms", "13", wavPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unsupported frame-ms") {
		t.Fatalf("expected stderr to mention unsupported frame-ms, got: %s", stderr.String())
	}
}

// Any WAV sample rate is accepted: libopus rates are encoded natively, others are resampled
// to 48 kHz. The decoded length is ceil(frames*48000/rate) and OpusHead records the real rate.
func TestWav2OggOpus_AcceptsAnySampleRate(t *testing.T) {
	for _, rate := range []int{8000, 16000, 24000, 44100, 22050} {
		frames := rate + 321
		wavPath := buildTestWAV(t, rate, 2, frames)
		outPath := filepath.Join(t.TempDir(), "out.opus")

		var stdout, stderr bytes.Buffer
		if code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr); code != 0 {
			t.Fatalf("rate %d: exit code %d (stderr=%q)", rate, code, stderr.String())
		}
		f, err := os.Open(outPath)
		if err != nil {
			t.Fatal(err)
		}
		r, err := ogg.NewOpusReader(f)
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		if int(r.Head.InputSampleRate) != rate {
			t.Errorf("rate %d: OpusHead.InputSampleRate = %d", rate, r.Head.InputSampleRate)
		}
		var last *ogg.OpusAudioPacket
		for {
			p, err := r.ReadAudioPacket()
			if err != nil {
				break
			}
			last = p
		}
		f.Close()
		want := (frames*48000 + rate - 1) / rate
		if last == nil || !last.EOS || int(last.GranulePos)-int(r.Head.PreSkip) != want {
			t.Errorf("rate %d: stream does not end at %d samples with EOS", rate, want)
		}
	}
}

func TestWav2OggOpus_ComplexityZeroIsAccepted(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", outPath, "--cpuprofile", "", "--complexity", "0", wavPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d (stderr=%q)", code, stderr.String())
	}
}

func TestWav2OggOpus_InvalidChannelCount(t *testing.T) {
	// wav.NewWriter itself does not validate channel count, so a 3-channel
	// WAV file can be constructed directly to exercise wav2oggopus's own
	// mono/stereo guard.
	wavPath := buildTestWAV(t, 48000, 3, 4800)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "only mono (1) and stereo (2)") {
		t.Fatalf("expected stderr to mention mono/stereo requirement, got: %s", stderr.String())
	}
}

func TestWav2OggOpus_MissingInputFile(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", filepath.Join(t.TempDir(), "does-not-exist.wav")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestWav2OggOpus_UsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cpuprofile", ""}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2 for missing input file arg, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected stderr to contain usage message, got: %s", stderr.String())
	}
}

func TestParseApplication(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"audio", opus.ApplicationAudio, false},
		{"voip", opus.ApplicationVoIP, false},
		{"lowdelay", opus.ApplicationRestrictedLowDelay, false},
		{"low-delay", opus.ApplicationRestrictedLowDelay, false},
		{"restricted-lowdelay", opus.ApplicationRestrictedLowDelay, false},
		{"App-VoIP", opus.ApplicationVoIP, false},
		{"bogus", 0, true},
	}
	for _, c := range cases {
		got, err := parseApplication(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseApplication(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseApplication(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseApplication(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCheckFrameMS(t *testing.T) {
	for _, ms := range []int{5, 10, 20, 40, 60} {
		if err := checkFrameMS(ms); err != nil {
			t.Errorf("checkFrameMS(%d): unexpected error: %v", ms, err)
		}
	}
	for _, ms := range []int{0, 2, 13, 25, 120, -20} {
		if err := checkFrameMS(ms); err == nil {
			t.Errorf("checkFrameMS(%d): expected an error", ms)
		}
	}
}

// RFC 7845 Section 4: interior granules count decoded samples without a
// pre-skip offset, they never decrease, and EOS carries PreSkip + input length.
func TestWav2OggOpus_GranulePositions(t *testing.T) {
	const frames = 120*960 + 700
	wavPath := buildTestWAV(t, 48000, 1, frames)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d (stderr=%q)", code, stderr.String())
	}
	outF, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer outF.Close()
	r, err := ogg.NewOpusReader(outF)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	var prev uint64
	var last *ogg.OpusAudioPacket
	for i := 1; ; i++ {
		pkt, err := r.ReadAudioPacket()
		if err != nil {
			break
		}
		if pkt.GranuleValid {
			if pkt.GranulePos < prev {
				t.Fatalf("packet %d: granule %d decreased from %d", i, pkt.GranulePos, prev)
			}
			prev = pkt.GranulePos
		}
		if pkt.GranuleValid && !pkt.EOS && pkt.GranulePos%960 != 0 {
			t.Errorf("interior granule %d is not a multiple of 960 (pre-skip leaked in)", pkt.GranulePos)
		}
		last = pkt
	}
	if last == nil || !last.EOS {
		t.Fatal("missing EOS packet")
	}
	if want := uint64(r.Head.PreSkip) + frames; last.GranulePos != want {
		t.Errorf("EOS granule = %d, want %d", last.GranulePos, want)
	}
}

func TestWav2OggOpus_BatchesPages(t *testing.T) {
	const frames = 4 * 48000
	wavPath := buildTestWAV(t, 48000, 1, frames)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d (stderr=%q)", code, stderr.String())
	}
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()

	pr := ogg.NewPageReader(f)
	var pages []*ogg.Page
	for {
		p, err := pr.ReadPage()
		if err != nil {
			break
		}
		pages = append(pages, p)
	}
	const packets = frames / 960
	if len(pages) < 4 || (len(pages)-2)*4 > packets {
		t.Fatalf("%d pages for %d audio packets: packets are not batched", len(pages), packets)
	}
	if !bytes.HasPrefix(pages[1].SegmentData, []byte("OpusTags")) || bytes.HasPrefix(pages[2].SegmentData, []byte("OpusTags")) {
		t.Errorf("OpusTags must sit alone on page 1, audio must start on page 2")
	}
	if !pages[len(pages)-1].IsEOS() {
		t.Errorf("last page is not EOS")
	}
}

func TestWav2OggOpus_RefusesToOverwriteInput(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	before, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", wavPath, "--cpuprofile", "", wavPath}, &stdout, &stderr); code == 0 {
		t.Fatal("expected a non-zero exit code when --out is the input file")
	}
	after, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("input WAV was modified")
	}
}

func TestWav2OggOpus_ReplacesExistingOutputAndLeavesNoTempFiles(t *testing.T) {
	wavPath := buildTestWAV(t, 48000, 1, 4800)
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "out.opus")
	if err := os.WriteFile(outPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d (stderr=%q)", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil || !bytes.HasPrefix(got, []byte("OggS")) {
		t.Fatalf("output not replaced (err=%v)", err)
	}
	if ents, _ := os.ReadDir(outDir); len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
}
