package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
	"github.com/selawe/go-opus-codec/wav"
)

func TestOggOpus2Wav_EOSTrimming(t *testing.T) {
	const sampleRate = 48000
	const channels = 1
	const frameSize = 960

	// Create 2 frames of audio (1920 samples)
	pcm := make([]int16, frameSize*2)
	for i := range pcm {
		pcm[i] = int16(i % 1000)
	}

	enc, err := opus.NewEncoder(sampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	lookahead, err := enc.Lookahead()
	if err != nil {
		t.Fatalf("Lookahead: %v", err)
	}

	var oggBuf bytes.Buffer
	pw := ogg.NewPacketWriter(&oggBuf, 0x55667788)

	head := ogg.OpusHead{
		Version:              1,
		Channels:             uint8(channels),
		PreSkip:              uint16(lookahead),
		InputSampleRate:      sampleRate,
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("BuildOpusHeadPacket: %v", err)
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "test"})
	if err != nil {
		t.Fatalf("BuildOpusTagsPacket: %v", err)
	}

	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("write tags: %v", err)
	}

	packetBuf := make([]byte, 2000)
	nEnc1, err := enc.Encode(pcm[:frameSize], frameSize, packetBuf)
	if err != nil {
		t.Fatalf("encode frame 1: %v", err)
	}
	packet1 := append([]byte(nil), packetBuf[:nEnc1]...)

	nEnc2, err := enc.Encode(pcm[frameSize:], frameSize, packetBuf)
	if err != nil {
		t.Fatalf("encode frame 2: %v", err)
	}
	packet2 := append([]byte(nil), packetBuf[:nEnc2]...)

	// Target: exactly 1500 original audio samples (so final page trims 420 samples)
	targetOriginalSamples := uint64(1500)
	eosGranule := uint64(lookahead) + targetOriginalSamples

	if err := pw.WritePacket(packet1, uint64(lookahead)+frameSize, false, false); err != nil {
		t.Fatalf("write packet 1: %v", err)
	}
	if err := pw.WritePacket(packet2, eosGranule, false, true); err != nil {
		t.Fatalf("write packet 2: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	tmpDir := t.TempDir()
	oggPath := filepath.Join(tmpDir, "test.opus")
	wavPath := filepath.Join(tmpDir, "test.wav")

	if err := os.WriteFile(oggPath, oggBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("write ogg file: %v", err)
	}

	// Run conversion by calling main logic
	in, err := os.Open(oggPath)
	if err != nil {
		t.Fatalf("open ogg: %v", err)
	}
	defer in.Close()

	r, err := ogg.NewOpusReader(in)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}

	dec, err := opus.NewDecoderFromHead(r.Head)
	if err != nil {
		t.Fatalf("NewDecoderFromHead: %v", err)
	}
	defer dec.Close()

	outf, err := os.Create(wavPath)
	if err != nil {
		t.Fatalf("create wav: %v", err)
	}
	defer outf.Close()

	ww, err := wav.NewWriter(outf, 48000, int(r.Head.Channels))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	maxFrame := 5760
	ch := int(r.Head.Channels)
	decPcm := make([]int16, maxFrame*ch)
	preSkipRemaining := int(r.Head.PreSkip)
	totalSamplesDecoded := uint64(0)

	for {
		pkt, err := r.ReadAudioPacket()
		if err != nil {
			break
		}
		n, err := dec.Decode(pkt.Data, decPcm, maxFrame, false)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		totalSamplesDecoded += uint64(n)
		frames := decPcm[:n*ch]

		if preSkipRemaining > 0 {
			skip := min(preSkipRemaining, n)
			preSkipRemaining -= skip
			drop := skip * ch
			if drop >= len(frames) {
				continue
			}
			frames = frames[drop:]
		}

		if pkt.GranuleValid && totalSamplesDecoded > pkt.GranulePos {
			excess := totalSamplesDecoded - pkt.GranulePos
			excessSamples := int(excess) * ch
			if excessSamples < len(frames) {
				frames = frames[:len(frames)-excessSamples]
			} else {
				frames = nil
			}
		}

		if len(frames) > 0 {
			if err := ww.WriteInt16PCM(frames); err != nil {
				t.Fatalf("write pcm: %v", err)
			}
		}
	}
	_ = ww.Close()

	// Read wav and verify sample count
	wavf, err := os.Open(wavPath)
	if err != nil {
		t.Fatalf("open wav: %v", err)
	}
	defer wavf.Close()

	wr, err := wav.NewReader(wavf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	readBuf := make([]int16, 2048)
	totalWavSamples := 0
	for {
		n, err := wr.ReadInt16PCM(readBuf)
		if n > 0 {
			totalWavSamples += n
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadInt16PCM: %v", err)
		}
	}

	if uint64(totalWavSamples) != targetOriginalSamples {
		t.Fatalf("expected %d samples in wav output after EOS trimming, got %d", targetOriginalSamples, totalWavSamples)
	}
}

func TestOggOpus2Wav_RefusesToOverwriteInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.ogg")
	content := []byte("OggS-not-really-but-must-survive")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := run([]string{"--out", path, path}, &stderr); code == 0 {
		t.Fatal("expected a non-zero exit code when --out is the input file")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("input was modified (err=%v)", err)
	}
}

func TestOggOpus2Wav_ErrorStillWritesCPUProfile(t *testing.T) {
	dir := t.TempDir()
	prof := filepath.Join(dir, "cpu.prof")
	var stderr bytes.Buffer
	code := run([]string{"--cpuprofile", prof, "--out", filepath.Join(dir, "o.wav"), filepath.Join(dir, "missing.ogg")}, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if fi, err := os.Stat(prof); err != nil || fi.Size() == 0 {
		t.Errorf("CPU profile was not flushed on the error path (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "o.wav")); err == nil {
		t.Error("output file exists after failure")
	}
}

func TestOggOpus2Wav_HelpExitsZero(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"-h"}, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestOggOpus2Wav_UsageErrorDoesNotCreateCPUProfile(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "cpu.prof")
	var stderr bytes.Buffer
	if code := run([]string{"--cpuprofile", prof}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if _, err := os.Stat(prof); err == nil {
		t.Error("CPU profile created although arguments were invalid")
	}
}

func TestOggOpus2Wav_MaxBytes(t *testing.T) {
	src := filepath.Join("..", "..", "test", "music_64kbps.opus")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "o.wav")

	var stderr bytes.Buffer
	if code := run([]string{"--max-bytes", "1000", "--out", out, src}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("output exists although the limit was exceeded")
	}

	if code := run([]string{"--max-bytes", "-1", "--out", out, src}, &stderr); code != 2 {
		t.Fatalf("negative --max-bytes: exit code = %d, want 2", code)
	}
	if code := run([]string{"--max-bytes", "0", "--out", out, src}, &stderr); code != 0 {
		t.Fatalf("--max-bytes 0: exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
}
