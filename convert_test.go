package opusgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/wav"
)

// memWriteSeeker implements io.WriteSeeker in memory for testing.
type memWriteSeeker struct {
	buf []byte
	pos int64
}

func (m *memWriteSeeker) Write(p []byte) (n int, err error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.buf)) {
		newBuf := make([]byte, end)
		copy(newBuf, m.buf)
		m.buf = newBuf
	}
	copy(m.buf[m.pos:], p)
	m.pos = end
	return len(p), nil
}

func (m *memWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = m.pos + offset
	case io.SeekEnd:
		newPos = int64(len(m.buf)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if newPos < 0 {
		return 0, errors.New("negative seek position")
	}
	m.pos = newPos
	return newPos, nil
}

func (m *memWriteSeeker) Bytes() []byte {
	return m.buf
}

func generateSineWAV(t *testing.T, sampleRate int, channels int, totalSamplesPerCh int) []byte {
	t.Helper()
	ws := &memWriteSeeker{}
	ww, err := wav.NewWriter(ws, sampleRate, channels)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	totalSamples := totalSamplesPerCh * channels
	pcm := make([]int16, totalSamples)
	freq := 440.0
	for i := 0; i < totalSamplesPerCh; i++ {
		val := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * 16000)
		for ch := 0; ch < channels; ch++ {
			pcm[i*channels+ch] = val
		}
	}

	if err := ww.WriteInt16PCM(pcm); err != nil {
		t.Fatalf("WriteInt16PCM: %v", err)
	}
	if err := ww.Close(); err != nil {
		t.Fatalf("wav Close: %v", err)
	}
	return ws.Bytes()
}

func readAllWAVSamples(t *testing.T, data []byte) (channels int, sampleRate int, totalSamplesPerCh int) {
	t.Helper()
	wr, err := wav.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("wav.NewReader: %v", err)
	}
	channels = wr.Channels()
	sampleRate = wr.SampleRate()

	buf := make([]int16, 1024)
	totalInterleaved := 0
	for {
		n, err := wr.ReadInt16PCM(buf)
		if n > 0 {
			totalInterleaved += n
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadInt16PCM: %v", err)
		}
	}
	totalSamplesPerCh = totalInterleaved / channels
	return channels, sampleRate, totalSamplesPerCh
}

func TestEncodeWAVToOggOpus_And_DecodeOggOpusToWAV_Mono(t *testing.T) {
	// Generate 1500 samples (non-frame-aligned at 20ms = 960 samples, tests EOS trimming)
	const sampleCount = 1500
	wavData := generateSineWAV(t, 48000, 1, sampleCount)

	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}
	if oggBuf.Len() == 0 {
		t.Fatalf("encoded ogg data is empty")
	}

	outWAV := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(&oggBuf, outWAV); err != nil {
		t.Fatalf("DecodeOggOpusToWAV: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, outWAV.Bytes())
	if ch != 1 {
		t.Errorf("expected 1 channel, got %d", ch)
	}
	if sr != 48000 {
		t.Errorf("expected 48000 Hz, got %d", sr)
	}
	if decodedSamples != sampleCount {
		t.Errorf("expected %d samples decoded after EOS trimming, got %d", sampleCount, decodedSamples)
	}
}

func TestEncodeWAVToOggOpus_And_DecodeOggOpusToWAV_Stereo_CustomOptions(t *testing.T) {
	const sampleCount = 2880 // exactly 1 frame of 60ms at 48kHz
	wavData := generateSineWAV(t, 48000, 2, sampleCount)

	opts := &EncodeOptions{
		Bitrate:     96000,
		CBR:         true,
		Complexity:  8,
		Application: ApplicationAudio,
		FrameSizeMS: 60,
		Vendor:      "custom-vendor",
		Comments:    []string{"TITLE=Test Stereo", "ARTIST=OpusGo"},
	}

	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, opts); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}

	outWAV := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(&oggBuf, outWAV); err != nil {
		t.Fatalf("DecodeOggOpusToWAV: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, outWAV.Bytes())
	if ch != 2 {
		t.Errorf("expected 2 channels, got %d", ch)
	}
	if sr != 48000 {
		t.Errorf("expected 48000 Hz, got %d", sr)
	}
	if decodedSamples != sampleCount {
		t.Errorf("expected %d samples decoded, got %d", sampleCount, decodedSamples)
	}
}

func TestConvertFileHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	wavInPath := filepath.Join(tmpDir, "input.wav")
	oggPath := filepath.Join(tmpDir, "encoded.opus")
	wavOutPath := filepath.Join(tmpDir, "output.wav")

	const sampleCount = 1920 // 2 frames of 20ms
	originalWAV := generateSineWAV(t, 48000, 1, sampleCount)
	if err := os.WriteFile(wavInPath, originalWAV, 0o644); err != nil {
		t.Fatalf("write input wav: %v", err)
	}

	if err := ConvertWAVFileToOggOpus(wavInPath, oggPath, nil); err != nil {
		t.Fatalf("ConvertWAVFileToOggOpus: %v", err)
	}

	if _, err := os.Stat(oggPath); err != nil {
		t.Fatalf("ogg file was not created: %v", err)
	}

	if err := ConvertOggOpusFileToWAV(oggPath, wavOutPath); err != nil {
		t.Fatalf("ConvertOggOpusFileToWAV: %v", err)
	}

	decodedWAV, err := os.ReadFile(wavOutPath)
	if err != nil {
		t.Fatalf("read decoded wav: %v", err)
	}

	ch, sr, decodedSamples := readAllWAVSamples(t, decodedWAV)
	if ch != 1 || sr != 48000 || decodedSamples != sampleCount {
		t.Errorf("decoded wav mismatch: ch=%d, sr=%d, samples=%d (expected 1, 48000, %d)",
			ch, sr, decodedSamples, sampleCount)
	}
}

func TestEncodeWAV_ValidationErrors(t *testing.T) {
	// Nil checks
	if err := EncodeWAVToOggOpus(nil, &bytes.Buffer{}, nil); err == nil {
		t.Error("expected error for nil wavReader, got nil")
	}
	if err := EncodeWAVToOggOpus(bytes.NewReader(nil), nil, nil); err == nil {
		t.Error("expected error for nil oggWriter, got nil")
	}

	// Any sample rate is accepted (44.1 kHz is resampled), but more than two channels is not.
	rate44k := generateSineWAV(t, 44100, 1, 1000)
	if err := EncodeWAVToOggOpus(bytes.NewReader(rate44k), &bytes.Buffer{}, nil); err != nil {
		t.Errorf("44.1 kHz WAV should be accepted, got %v", err)
	}

	// Unsupported frame duration
	validWAV := generateSineWAV(t, 48000, 1, 1000)
	opts := &EncodeOptions{FrameSizeMS: 25}
	if err := EncodeWAVToOggOpus(bytes.NewReader(validWAV), &bytes.Buffer{}, opts); err == nil {
		t.Error("expected error for 25ms frame duration, got nil")
	}
}

func TestDecodeOggOpus_ValidationErrors(t *testing.T) {
	// Nil checks
	if err := DecodeOggOpusToWAV(nil, &memWriteSeeker{}); err == nil {
		t.Error("expected error for nil oggReader, got nil")
	}
	if err := DecodeOggOpusToWAV(bytes.NewReader(nil), nil); err == nil {
		t.Error("expected error for nil wavWriter, got nil")
	}

	// Corrupted Ogg data
	corruptData := []byte("not an ogg stream at all")
	if err := DecodeOggOpusToWAV(bytes.NewReader(corruptData), &memWriteSeeker{}); err == nil {
		t.Error("expected error for corrupt ogg stream, got nil")
	}
}

func TestDecodeOggOpusToWAV_OutputLimit(t *testing.T) {
	wavData := generateSineWAV(t, 48000, 1, 1500)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus: %v", err)
	}

	outWAV := &memWriteSeeker{}
	// 1500 samples * 2 bytes = 3000 bytes. Limit to 1000 bytes.
	err := DecodeOggOpusToWAV(&oggBuf, outWAV, WithMaxOutputBytes(1000))
	if !errors.Is(err, ErrOutputLimitExceeded) {
		t.Fatalf("expected ErrOutputLimitExceeded, got %v", err)
	}
}

func TestConvertFiles_CleanupOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	badSrc := filepath.Join(tmpDir, "bad.wav")
	dstOgg := filepath.Join(tmpDir, "out.ogg")
	if err := os.WriteFile(badSrc, []byte("not a wav file"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ConvertWAVFileToOggOpus(badSrc, dstOgg, nil); err == nil {
		t.Fatal("expected error converting invalid WAV")
	}
	if _, err := os.Stat(dstOgg); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected %s to be cleaned up, but it exists", dstOgg)
	}

	badOgg := filepath.Join(tmpDir, "bad.ogg")
	dstWAV := filepath.Join(tmpDir, "out.wav")
	if err := os.WriteFile(badOgg, []byte("not an ogg file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConvertOggOpusFileToWAV(badOgg, dstWAV); err == nil {
		t.Fatal("expected error converting invalid Ogg")
	}
	if _, err := os.Stat(dstWAV); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected %s to be cleaned up, but it exists", dstWAV)
	}
}

func TestEncodeWAVToOggOpus_EmptyInputHasEOS(t *testing.T) {
	emptyWAV := generateSineWAV(t, 48000, 1, 0)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(emptyWAV), &oggBuf, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus empty: %v", err)
	}

	pr := ogg.NewPageReader(&oggBuf)
	var hasEOS bool
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		if p.IsEOS() {
			hasEOS = true
		}
	}
	if !hasEOS {
		t.Fatal("empty input was encoded without EOS page")
	}
}

// RFC 7845 Section 4: a page's granule position counts every sample decoded so
// far, pre-skip included, so interior pages carry frames*frameSize and only the
// EOS page carries the pre-skip plus the true input length.
func TestEncodeWAVToOggOpus_GranulePositions(t *testing.T) {
	const frameSize = 960 // default 20 ms at 48 kHz
	for _, n := range []int{1900, 2 * frameSize, 43000, 48000} {
		src := generateSineWAV(t, 48000, 1, n)
		var oggBuf bytes.Buffer
		if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
			t.Fatalf("n=%d: encode: %v", n, err)
		}
		r, err := ogg.NewOpusReader(&oggBuf)
		if err != nil {
			t.Fatalf("n=%d: NewOpusReader: %v", n, err)
		}
		var (
			count int
			prev  uint64
			last  *ogg.OpusAudioPacket
		)
		for {
			pkt, err := r.ReadAudioPacket()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("n=%d: ReadAudioPacket: %v", n, err)
			}
			count++
			if pkt.GranuleValid {
				if pkt.GranulePos < prev {
					t.Fatalf("n=%d: packet %d: granule %d decreased from %d", n, count, pkt.GranulePos, prev)
				}
				prev = pkt.GranulePos
			}
			last = pkt
		}
		if last == nil || !last.EOS {
			t.Fatalf("n=%d: last packet missing EOS", n)
		}
		if want := uint64(r.Head.PreSkip) + uint64(n); last.GranulePos != want {
			t.Errorf("n=%d: EOS granule = %d, want %d", n, last.GranulePos, want)
		}
	}
}

func TestEncodeWAVToOggOpus_InteriorGranuleExcludesPreSkip(t *testing.T) {
	const frameSize = 960
	src := generateSineWAV(t, 48000, 1, 120*frameSize)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	r, err := ogg.NewOpusReader(&oggBuf)
	if err != nil {
		t.Fatalf("NewOpusReader: %v", err)
	}
	for {
		pkt, err := r.ReadAudioPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadAudioPacket: %v", err)
		}
		if pkt.GranuleValid && !pkt.EOS && pkt.GranulePos%frameSize != 0 {
			t.Errorf("interior granule %d is not a multiple of %d (pre-skip leaked in)", pkt.GranulePos, frameSize)
		}
	}
}

// Framing must batch audio packets (RFC 3533 Section 6) while keeping the
// RFC 7845 layout: OpusHead alone on the BOS page, OpusTags ending its page,
// and audio starting on a fresh page.
func TestEncodeWAVToOggOpus_BatchesPages(t *testing.T) {
	const n = 4 * 48000
	src := generateSineWAV(t, 48000, 1, n)
	var oggBuf bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw := append([]byte(nil), oggBuf.Bytes()...)

	pr := ogg.NewPageReader(bytes.NewReader(raw))
	var pages []*ogg.Page
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		pages = append(pages, p)
	}
	if len(pages) < 4 {
		t.Fatalf("got %d pages, want at least 4", len(pages))
	}
	if !pages[0].IsBOS() || !bytes.HasPrefix(pages[0].SegmentData, []byte("OpusHead")) || len(pages[0].SegmentTable) > 2 {
		t.Errorf("page 0 is not a lone OpusHead BOS page")
	}
	if !bytes.HasPrefix(pages[1].SegmentData, []byte("OpusTags")) {
		t.Fatalf("page 1 does not start with OpusTags")
	}
	if bytes.HasPrefix(pages[2].SegmentData, []byte("OpusTags")) || pages[2].IsContinuedPacket() {
		t.Errorf("first audio page must start a fresh packet on its own page")
	}
	if !pages[len(pages)-1].IsEOS() {
		t.Errorf("last page is not EOS")
	}

	audioPages := len(pages) - 2
	const packets = n / 960
	if audioPages*4 > packets {
		t.Errorf("%d audio pages for %d packets: packets are not batched", audioPages, packets)
	}
	var prev uint64
	for i, p := range pages[2:] {
		if p.GranulePosition < prev {
			t.Fatalf("audio page %d: granule %d decreased from %d", i, p.GranulePosition, prev)
		}
		prev = p.GranulePosition
		if i < audioPages-1 && len(p.SegmentTable) > 0 {
			if dur := len(p.SegmentTable); dur > 60 {
				t.Errorf("audio page %d holds %d packets (over ~1 s)", i, dur)
			}
		}
	}

	// Batched pages must still round-trip to the exact input length.
	var wavOut memWriteSeeker
	if err := DecodeOggOpusToWAV(bytes.NewReader(raw), &wavOut); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, got := readAllWAVSamples(t, wavOut.Bytes()); got != n {
		t.Errorf("decoded %d samples, want %d", got, n)
	}
}

func TestConvertFiles_NeverDestroyExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Refuse to write over the input.
	wavPath := filepath.Join(tmpDir, "in.wav")
	if err := os.WriteFile(wavPath, generateSineWAV(t, 48000, 1, 4800), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(wavPath)
	if err := ConvertWAVFileToOggOpus(wavPath, wavPath, nil); err == nil {
		t.Fatal("expected an error when dst == src")
	}
	if after, _ := os.ReadFile(wavPath); !bytes.Equal(before, after) {
		t.Fatal("input WAV was modified")
	}

	oggPath := filepath.Join(tmpDir, "in.ogg")
	if err := ConvertWAVFileToOggOpus(wavPath, oggPath, nil); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(oggPath)
	if err := ConvertOggOpusFileToWAV(oggPath, oggPath); err == nil {
		t.Fatal("expected an error when dst == src")
	}
	if after, _ := os.ReadFile(oggPath); !bytes.Equal(before, after) {
		t.Fatal("input Ogg was modified")
	}

	// A failed conversion keeps a pre-existing destination intact.
	bad := filepath.Join(tmpDir, "bad.wav")
	if err := os.WriteFile(bad, []byte("not a wav file"), 0600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmpDir, "keep.ogg")
	if err := os.WriteFile(dst, []byte("previous good output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConvertWAVFileToOggOpus(bad, dst, nil); err == nil {
		t.Fatal("expected error")
	}
	if got, _ := os.ReadFile(dst); string(got) != "previous good output" {
		t.Fatalf("existing destination was destroyed: %q", got)
	}
	if ents, _ := os.ReadDir(tmpDir); len(ents) != 4 {
		t.Errorf("unexpected leftover files: %v", ents)
	}

	// A successful conversion replaces it.
	if err := ConvertWAVFileToOggOpus(wavPath, dst, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.HasPrefix(got, []byte("OggS")) {
		t.Fatal("destination was not replaced by the new output")
	}
}

// decodedWAVPCM decodes an Ogg Opus stream to its (always 48 kHz) 16-bit PCM.
func decodedWAVPCM(t *testing.T, ogg []byte) (pcm []int16, channels int) {
	t.Helper()
	var out memWriteSeeker
	if err := DecodeOggOpusToWAV(bytes.NewReader(ogg), &out); err != nil {
		t.Fatalf("DecodeOggOpusToWAV: %v", err)
	}
	wr, err := wav.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if wr.SampleRate() != 48000 {
		t.Fatalf("decoded sample rate = %d, want 48000", wr.SampleRate())
	}
	buf := make([]int16, 4096)
	for {
		n, err := wr.ReadInt16PCM(buf)
		pcm = append(pcm, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return pcm, wr.Channels()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// dominantHz estimates channel 0's frequency from zero crossings, ignoring the edges.
func dominantHz(pcm []int16, channels int) float64 {
	frames := len(pcm) / channels
	lo, hi := frames/10, frames*9/10
	n := 0
	for i := lo + 1; i < hi; i++ {
		if (pcm[(i-1)*channels] < 0) != (pcm[i*channels] < 0) {
			n++
		}
	}
	return float64(n) / 2 / (float64(hi-lo) / 48000)
}

// Every input rate is accepted: 8/12/16/24/48 kHz are encoded natively, anything else is
// resampled to 48 kHz. The decoded stream always has exactly ceil(n*48000/rate) samples per
// channel, keeps the pitch, records the real input rate, and has valid granule positions.
func TestEncodeWAVToOggOpus_AnyInputSampleRate(t *testing.T) {
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000, 44100, 22050, 32000, 11025} {
		for _, channels := range []int{1, 2} {
			for _, n := range []int{0, 1, 1900, rate + 137} {
				name := fmt.Sprintf("%dHz/%dch/%dframes", rate, channels, n)
				src := generateSineWAV(t, rate, channels, n)
				var oggBuf bytes.Buffer
				if err := EncodeWAVToOggOpus(bytes.NewReader(src), &oggBuf, nil); err != nil {
					t.Fatalf("%s: encode: %v", name, err)
				}
				raw := append([]byte(nil), oggBuf.Bytes()...)

				r, err := ogg.NewOpusReader(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if int(r.Head.InputSampleRate) != rate {
					t.Errorf("%s: OpusHead.InputSampleRate = %d, want %d", name, r.Head.InputSampleRate, rate)
				}
				if r.Head.PreSkip != 312 {
					t.Errorf("%s: PreSkip = %d, want 312 (48 kHz units)", name, r.Head.PreSkip)
				}

				want := (n*48000 + rate - 1) / rate
				var prev uint64
				var last *ogg.OpusAudioPacket
				for {
					p, err := r.ReadAudioPacket()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					if p.GranuleValid {
						if p.GranulePos < prev {
							t.Fatalf("%s: granule decreased (%d after %d)", name, p.GranulePos, prev)
						}
						prev = p.GranulePos
					}
					last = p
				}
				if last == nil || !last.EOS {
					t.Fatalf("%s: stream does not end with EOS", name)
				}
				if got := int(last.GranulePos) - int(r.Head.PreSkip); got != want {
					t.Errorf("%s: EOS granule implies %d samples, want %d", name, got, want)
				}

				pcm, ch := decodedWAVPCM(t, raw)
				if got := len(pcm) / ch; got != want {
					t.Errorf("%s: decoded %d samples per channel, want %d", name, got, want)
				}
				if n == rate+137 {
					if hz := dominantHz(pcm, ch); hz < 435 || hz > 445 {
						t.Errorf("%s: decoded pitch %.1f Hz, want about 440", name, hz)
					}
				}
			}
		}
	}
}

func TestEncodeWAVToOggOpus_RejectsMoreThanTwoChannels(t *testing.T) {
	src := generateSineWAV(t, 48000, 3, 480)
	if err := EncodeWAVToOggOpus(bytes.NewReader(src), io.Discard, nil); err == nil {
		t.Fatal("expected an error for a 3-channel WAV")
	}
}

func TestEncodeOptions_ComplexityExplicitZero(t *testing.T) {
	src := generateSineWAV(t, 48000, 2, 48000)
	encode := func(o EncodeOptions) []byte {
		o.Serial = 1 // same stream serial and fixed tags so only the audio can differ
		o.Comments = []string{"A=b"}
		var buf bytes.Buffer
		if err := EncodeWAVToOggOpus(bytes.NewReader(src), &buf, &o); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	def := encode(EncodeOptions{})                                             // unset: complexity 10
	zeroUnset := encode(EncodeOptions{Complexity: 0})                          // 0 without the flag: still 10
	zero := encode(EncodeOptions{Complexity: 0, ComplexityExplicit: true})     // really 0
	ten := encode(EncodeOptions{Complexity: 10, ComplexityExplicit: true})     // explicit 10
	tooHigh := encode(EncodeOptions{Complexity: 11, ComplexityExplicit: true}) // out of range: default
	if !bytes.Equal(def, zeroUnset) || !bytes.Equal(def, ten) || !bytes.Equal(def, tooHigh) {
		t.Error("unset, explicit 10 and out-of-range complexity should all mean complexity 10")
	}
	if bytes.Equal(def, zero) {
		t.Error("ComplexityExplicit with 0 produced the same stream as complexity 10")
	}
}

// A 700001 Hz header (under the WAV cap, coprime with 48000) would need a multi-GB resampler table.
func TestEncodeWAVRejectsUnresamplableRate(t *testing.T) {
	wavData := generateSineWAV(t, 700001, 1, 1000)
	err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &bytes.Buffer{}, nil)
	if err == nil {
		t.Fatal("expected error for unresamplable sample rate")
	}
}

// Resampling must preserve duration: one second at 44.1 kHz comes back as one second at 48 kHz.
func TestEncodeDecode44100KeepsDuration(t *testing.T) {
	const seconds = 2
	wavData := generateSineWAV(t, 44100, 1, 44100*seconds)

	var encoded bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavData), &encoded, nil); err != nil {
		t.Fatal(err)
	}
	ws := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(bytes.NewReader(encoded.Bytes()), ws); err != nil {
		t.Fatal(err)
	}

	r, err := wav.NewReader(bytes.NewReader(ws.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	buf := make([]int16, 4800)
	for {
		n, err := r.ReadInt16PCM(buf)
		total += n
		if err != nil {
			break
		}
	}
	if want := 48000 * seconds; total < want-48 || total > want+48 { // a frame of slack
		t.Fatalf("decoded %d samples, want about %d", total, want)
	}
}

func TestEncode24BitWAVToOggOpus(t *testing.T) {
	// Generate 1 second of 24-bit stereo 48 kHz sine wave
	const sampleRate = 48000
	const channels = 2
	const numSamplesPerCh = 48000
	var audioData bytes.Buffer
	for i := 0; i < numSamplesPerCh; i++ {
		val := math.Sin(2 * math.Pi * 440.0 * float64(i) / float64(sampleRate))
		s24 := int32(val * 8000000.0)
		for c := 0; c < channels; c++ {
			var b [3]byte
			b[0] = byte(s24)
			b[1] = byte(s24 >> 8)
			b[2] = byte(s24 >> 16)
			audioData.Write(b[:])
		}
	}

	blockAlign := uint16(channels * 3)
	byteRate := uint32(sampleRate) * uint32(blockAlign)
	var fmtBuf [16]byte
	binary.LittleEndian.PutUint16(fmtBuf[0:2], wav.FormatPCM)
	binary.LittleEndian.PutUint16(fmtBuf[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtBuf[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtBuf[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtBuf[12:14], blockAlign)
	binary.LittleEndian.PutUint16(fmtBuf[14:16], 24)

	var wavBuf bytes.Buffer
	wavBuf.WriteString("RIFF")
	totalRIFF := uint32(4 + 8 + len(fmtBuf) + 8 + audioData.Len())
	_ = binary.Write(&wavBuf, binary.LittleEndian, totalRIFF)
	wavBuf.WriteString("WAVE")
	wavBuf.WriteString("fmt ")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(len(fmtBuf)))
	wavBuf.Write(fmtBuf[:])
	wavBuf.WriteString("data")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(audioData.Len()))
	wavBuf.Write(audioData.Bytes())

	var encoded bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavBuf.Bytes()), &encoded, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus with 24-bit WAV failed: %v", err)
	}

	ws := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(bytes.NewReader(encoded.Bytes()), ws); err != nil {
		t.Fatalf("DecodeOggOpusToWAV failed: %v", err)
	}
	r, err := wav.NewReader(bytes.NewReader(ws.Bytes()))
	if err != nil {
		t.Fatalf("wav.NewReader on decoded WAV failed: %v", err)
	}
	if r.Channels() != channels || r.SampleRate() != 48000 {
		t.Errorf("decoded WAV channels=%d, rate=%d", r.Channels(), r.SampleRate())
	}
}

func TestEncode32BitFloatWAVToOggOpus(t *testing.T) {
	// Generate 1 second of 32-bit float stereo 48 kHz sine wave
	const sampleRate = 48000
	const channels = 2
	const numSamplesPerCh = 48000
	var audioData bytes.Buffer
	for i := 0; i < numSamplesPerCh; i++ {
		val := float32(math.Sin(2*math.Pi*440.0*float64(i)/float64(sampleRate)) * 0.8)
		for c := 0; c < channels; c++ {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(val))
			audioData.Write(b[:])
		}
	}

	blockAlign := uint16(channels * 4)
	byteRate := uint32(sampleRate) * uint32(blockAlign)
	var fmtBuf [16]byte
	binary.LittleEndian.PutUint16(fmtBuf[0:2], wav.FormatIEEEFloat)
	binary.LittleEndian.PutUint16(fmtBuf[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtBuf[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtBuf[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtBuf[12:14], blockAlign)
	binary.LittleEndian.PutUint16(fmtBuf[14:16], 32)

	var wavBuf bytes.Buffer
	wavBuf.WriteString("RIFF")
	totalRIFF := uint32(4 + 8 + len(fmtBuf) + 8 + audioData.Len())
	_ = binary.Write(&wavBuf, binary.LittleEndian, totalRIFF)
	wavBuf.WriteString("WAVE")
	wavBuf.WriteString("fmt ")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(len(fmtBuf)))
	wavBuf.Write(fmtBuf[:])
	wavBuf.WriteString("data")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(audioData.Len()))
	wavBuf.Write(audioData.Bytes())

	var encoded bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavBuf.Bytes()), &encoded, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus with float32 WAV failed: %v", err)
	}

	ws := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(bytes.NewReader(encoded.Bytes()), ws); err != nil {
		t.Fatalf("DecodeOggOpusToWAV failed: %v", err)
	}
	r, err := wav.NewReader(bytes.NewReader(ws.Bytes()))
	if err != nil {
		t.Fatalf("wav.NewReader on decoded WAV failed: %v", err)
	}
	if r.Channels() != channels || r.SampleRate() != 48000 {
		t.Errorf("decoded WAV channels=%d, rate=%d", r.Channels(), r.SampleRate())
	}
}

func TestEncodeRF64ToOggOpus(t *testing.T) {
	// Build an RF64 file containing 16-bit mono audio
	const sampleRate = 48000
	const channels = 1
	const numSamples = 48000
	var audioData bytes.Buffer
	for i := 0; i < numSamples; i++ {
		val := int16(math.Sin(2*math.Pi*440.0*float64(i)/float64(sampleRate)) * 16000)
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(val))
		audioData.Write(b[:])
	}

	blockAlign := uint16(channels * 2)
	byteRate := uint32(sampleRate) * uint32(blockAlign)
	var fmtBuf [16]byte
	binary.LittleEndian.PutUint16(fmtBuf[0:2], wav.FormatPCM)
	binary.LittleEndian.PutUint16(fmtBuf[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtBuf[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtBuf[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtBuf[12:14], blockAlign)
	binary.LittleEndian.PutUint16(fmtBuf[14:16], 16)

	var ds64Buf [28]byte
	binary.LittleEndian.PutUint64(ds64Buf[0:8], uint64(audioData.Len()+100))
	binary.LittleEndian.PutUint64(ds64Buf[8:16], uint64(audioData.Len()))
	binary.LittleEndian.PutUint64(ds64Buf[16:24], uint64(numSamples))
	binary.LittleEndian.PutUint32(ds64Buf[24:28], 0)

	var wavBuf bytes.Buffer
	wavBuf.WriteString("RF64")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	wavBuf.WriteString("WAVE")
	wavBuf.WriteString("ds64")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(len(ds64Buf)))
	wavBuf.Write(ds64Buf[:])
	wavBuf.WriteString("fmt ")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(len(fmtBuf)))
	wavBuf.Write(fmtBuf[:])
	wavBuf.WriteString("data")
	_ = binary.Write(&wavBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	wavBuf.Write(audioData.Bytes())

	var encoded bytes.Buffer
	if err := EncodeWAVToOggOpus(bytes.NewReader(wavBuf.Bytes()), &encoded, nil); err != nil {
		t.Fatalf("EncodeWAVToOggOpus with RF64 failed: %v", err)
	}

	ws := &memWriteSeeker{}
	if err := DecodeOggOpusToWAV(bytes.NewReader(encoded.Bytes()), ws); err != nil {
		t.Fatalf("DecodeOggOpusToWAV failed: %v", err)
	}
}
