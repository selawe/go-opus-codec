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

func TestWav2OggOpus_WrongSampleRate(t *testing.T) {
	wavPath := buildTestWAV(t, 44100, 1, 4410)
	outPath := filepath.Join(t.TempDir(), "out.opus")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", outPath, "--cpuprofile", "", wavPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "only 48kHz WAV supported") {
		t.Fatalf("expected stderr to mention 48kHz requirement, got: %s", stderr.String())
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
	if !strings.Contains(stderr.String(), "only mono and stereo") {
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

func TestFrameSizeFromMS(t *testing.T) {
	cases := []struct {
		ms      int
		want    int
		wantErr bool
	}{
		{5, 240, false},
		{10, 480, false},
		{20, 960, false},
		{40, 1920, false},
		{60, 2880, false},
		{13, 0, true},
	}
	for _, c := range cases {
		got, err := frameSizeFromMS(c.ms)
		if c.wantErr {
			if err == nil {
				t.Errorf("frameSizeFromMS(%d): expected error, got nil", c.ms)
			}
			continue
		}
		if err != nil {
			t.Errorf("frameSizeFromMS(%d): unexpected error: %v", c.ms, err)
			continue
		}
		if got != c.want {
			t.Errorf("frameSizeFromMS(%d) = %d, want %d", c.ms, got, c.want)
		}
	}
}
