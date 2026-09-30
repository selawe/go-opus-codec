package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

// Helper to construct a custom WAV in memory.
func buildWAVBytes(riffID string, chunks []struct {
	id   string
	data []byte
}) []byte {
	var body bytes.Buffer
	for _, c := range chunks {
		body.WriteString(c.id)
		sz := uint32(len(c.data))
		_ = binary.Write(&body, binary.LittleEndian, sz)
		body.Write(c.data)
		if len(c.data)%2 == 1 {
			body.WriteByte(0)
		}
	}

	var out bytes.Buffer
	out.WriteString(riffID)
	totalRIFF := uint32(4 + body.Len())
	if riffID == "RF64" || riffID == "BW64" {
		totalRIFF = 0xFFFFFFFF
	}
	_ = binary.Write(&out, binary.LittleEndian, totalRIFF)
	out.WriteString("WAVE")
	out.Write(body.Bytes())
	return out.Bytes()
}

func buildFmtChunk(format, channels uint16, sampleRate uint32, bitsPerSample uint16) []byte {
	blockAlign := channels * (bitsPerSample / 8)
	byteRate := sampleRate * uint32(blockAlign)
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint16(buf[0:2], format)
	binary.LittleEndian.PutUint16(buf[2:4], channels)
	binary.LittleEndian.PutUint32(buf[4:8], sampleRate)
	binary.LittleEndian.PutUint32(buf[8:12], byteRate)
	binary.LittleEndian.PutUint16(buf[12:14], blockAlign)
	binary.LittleEndian.PutUint16(buf[14:16], bitsPerSample)
	return buf
}

func TestRead8BitPCM(t *testing.T) {
	fmtData := buildFmtChunk(FormatPCM, 1, 48000, 8)
	// 8-bit PCM is unsigned: 0 is -128 (-32768 in int16), 128 is 0, 255 is +127 (+32512 in int16)
	audioData := []byte{0, 64, 128, 192, 255}
	wavBytes := buildWAVBytes("RIFF", []struct {
		id   string
		data []byte
	}{
		{"fmt ", fmtData},
		{"data", audioData},
	})

	r, err := NewReader(bytes.NewReader(wavBytes))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	if r.BitsPerSample() != 8 {
		t.Errorf("expected 8 bits, got %d", r.BitsPerSample())
	}
	if r.Channels() != 1 {
		t.Errorf("expected 1 channel, got %d", r.Channels())
	}

	pcm16 := make([]int16, 5)
	n, err := r.ReadInt16PCM(pcm16)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadInt16PCM failed: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 samples, got %d", n)
	}
	if pcm16[0] != -32768 || pcm16[2] != 0 {
		t.Errorf("unexpected 8-bit int16 samples: %v", pcm16)
	}

	// Test ReadFloat32PCM on a new reader
	r2, _ := NewReader(bytes.NewReader(wavBytes))
	pcmF32 := make([]float32, 5)
	nF, err := r2.ReadFloat32PCM(pcmF32)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFloat32PCM failed: %v", err)
	}
	if nF != 5 {
		t.Fatalf("expected 5 samples, got %d", nF)
	}
	if pcmF32[0] != -1.0 || pcmF32[2] != 0.0 {
		t.Errorf("unexpected 8-bit float samples: %v", pcmF32)
	}
}

func TestRead24BitPCM(t *testing.T) {
	fmtData := buildFmtChunk(FormatPCM, 2, 48000, 24)
	// Stereo 24-bit samples: 2 frames = 4 samples = 12 bytes
	// Frame 0: L=0, R=8388607 (max pos, 0x7FFFFF)
	// Frame 1: L=-8388608 (min neg, 0x800000), R=4194304 (0x400000)
	var audioData []byte
	// L: 0
	audioData = append(audioData, 0x00, 0x00, 0x00)
	// R: 0x7FFFFF
	audioData = append(audioData, 0xFF, 0xFF, 0x7F)
	// L: 0x800000
	audioData = append(audioData, 0x00, 0x00, 0x80)
	// R: 0x400000
	audioData = append(audioData, 0x00, 0x00, 0x40)

	wavBytes := buildWAVBytes("RIFF", []struct {
		id   string
		data []byte
	}{
		{"fmt ", fmtData},
		{"data", audioData},
	})

	r, err := NewReader(bytes.NewReader(wavBytes))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	if r.BitsPerSample() != 24 {
		t.Errorf("expected 24 bits, got %d", r.BitsPerSample())
	}
	if r.Channels() != 2 {
		t.Errorf("expected 2 channels, got %d", r.Channels())
	}

	pcm16 := make([]int16, 4)
	n, err := r.ReadInt16PCM(pcm16)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadInt16PCM failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 samples, got %d", n)
	}
	if pcm16[0] != 0 || pcm16[1] != 32767 || pcm16[2] != -32768 || pcm16[3] != 16384 {
		t.Errorf("unexpected 24-bit int16 samples: %v", pcm16)
	}

	// Test ReadFloat32PCM
	r2, _ := NewReader(bytes.NewReader(wavBytes))
	pcmF32 := make([]float32, 4)
	nF, err := r2.ReadFloat32PCM(pcmF32)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFloat32PCM failed: %v", err)
	}
	if nF != 4 {
		t.Fatalf("expected 4 samples, got %d", nF)
	}
	if pcmF32[0] != 0 || pcmF32[2] != -1.0 || math.Abs(float64(pcmF32[3])-0.5) > 1e-6 {
		t.Errorf("unexpected 24-bit float samples: %v", pcmF32)
	}
}

func TestRead32BitFloat(t *testing.T) {
	fmtData := buildFmtChunk(FormatIEEEFloat, 1, 48000, 32)
	var audioData []byte
	samples := []float32{0.0, 1.0, -1.0, 0.5, -0.5}
	for _, f := range samples {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(f))
		audioData = append(audioData, b[:]...)
	}

	wavBytes := buildWAVBytes("RIFF", []struct {
		id   string
		data []byte
	}{
		{"fmt ", fmtData},
		{"data", audioData},
	})

	r, err := NewReader(bytes.NewReader(wavBytes))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	if r.BitsPerSample() != 32 || r.Format() != FormatIEEEFloat {
		t.Errorf("expected 32-bit float, got %d bits, format %d", r.BitsPerSample(), r.Format())
	}

	pcmF32 := make([]float32, 5)
	nF, err := r.ReadFloat32PCM(pcmF32)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFloat32PCM failed: %v", err)
	}
	if nF != 5 {
		t.Fatalf("expected 5 samples, got %d", nF)
	}
	for i, expected := range samples {
		if pcmF32[i] != expected {
			t.Errorf("sample %d: expected %f, got %f", i, expected, pcmF32[i])
		}
	}

	// Test ReadInt16PCM conversion with clamping
	r2, _ := NewReader(bytes.NewReader(wavBytes))
	pcm16 := make([]int16, 5)
	n16, _ := r2.ReadInt16PCM(pcm16)
	if n16 != 5 {
		t.Fatalf("expected 5 samples, got %d", n16)
	}
	if pcm16[0] != 0 || pcm16[1] != 32767 || pcm16[2] != -32768 {
		t.Errorf("unexpected float->int16 conversion: %v", pcm16)
	}
}

func TestReadExtensibleFloatAndPCM(t *testing.T) {
	// Build WAVE_FORMAT_EXTENSIBLE fmt chunk (40 bytes)
	buildExtFmt := func(subformat uint32, bits uint16) []byte {
		buf := make([]byte, 40)
		binary.LittleEndian.PutUint16(buf[0:2], FormatExtensible)
		binary.LittleEndian.PutUint16(buf[2:4], 2)     // stereo
		binary.LittleEndian.PutUint32(buf[4:8], 48000) // rate
		blockAlign := 2 * (bits / 8)
		binary.LittleEndian.PutUint32(buf[8:12], 48000*uint32(blockAlign))
		binary.LittleEndian.PutUint16(buf[12:14], blockAlign)
		binary.LittleEndian.PutUint16(buf[14:16], bits)
		binary.LittleEndian.PutUint16(buf[16:18], 22)   // cbSize = 22
		binary.LittleEndian.PutUint16(buf[18:20], bits) // validBits
		binary.LittleEndian.PutUint32(buf[20:24], 3)    // channelMask (stereo)
		binary.LittleEndian.PutUint32(buf[24:28], subformat)
		copy(buf[28:40], []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71})
		return buf
	}

	// 1. Extensible 32-bit Float
	extFloatFmt := buildExtFmt(uint32(FormatIEEEFloat), 32)
	var floatData [8]byte // 1 stereo frame
	binary.LittleEndian.PutUint32(floatData[0:4], math.Float32bits(0.5))
	binary.LittleEndian.PutUint32(floatData[4:8], math.Float32bits(-0.5))

	wavBytes := buildWAVBytes("RIFF", []struct {
		id   string
		data []byte
	}{
		{"fmt ", extFloatFmt},
		{"data", floatData[:]},
	})

	r, err := NewReader(bytes.NewReader(wavBytes))
	if err != nil {
		t.Fatalf("NewReader extensible float failed: %v", err)
	}
	if r.Format() != FormatIEEEFloat {
		t.Errorf("expected FormatIEEEFloat, got %d", r.Format())
	}
	fDst := make([]float32, 2)
	n, _ := r.ReadFloat32PCM(fDst)
	if n != 2 || fDst[0] != 0.5 || fDst[1] != -0.5 {
		t.Errorf("unexpected extensible float: %v", fDst)
	}

	// 2. Extensible 24-bit PCM
	ext24Fmt := buildExtFmt(uint32(FormatPCM), 24)
	var pcm24Data [6]byte                                       // 1 stereo frame of 24-bit
	pcm24Data[0], pcm24Data[1], pcm24Data[2] = 0x00, 0x00, 0x40 // 0x400000 = +0.5
	pcm24Data[3], pcm24Data[4], pcm24Data[5] = 0x00, 0x00, 0xC0 // 0xC00000 = -0.5

	wav24Bytes := buildWAVBytes("RIFF", []struct {
		id   string
		data []byte
	}{
		{"fmt ", ext24Fmt},
		{"data", pcm24Data[:]},
	})
	r24, err := NewReader(bytes.NewReader(wav24Bytes))
	if err != nil {
		t.Fatalf("NewReader extensible 24-bit failed: %v", err)
	}
	if r24.BitsPerSample() != 24 || r24.Format() != FormatPCM {
		t.Errorf("expected 24-bit PCM, got %d bits, format %d", r24.BitsPerSample(), r24.Format())
	}
	f24Dst := make([]float32, 2)
	n24, _ := r24.ReadFloat32PCM(f24Dst)
	if n24 != 2 || math.Abs(float64(f24Dst[0])-0.5) > 1e-6 || math.Abs(float64(f24Dst[1])-(-0.5)) > 1e-6 {
		t.Errorf("unexpected extensible 24-bit float: %v", f24Dst)
	}
}

func TestReadRF64WithDS64(t *testing.T) {
	fmtData := buildFmtChunk(FormatPCM, 1, 48000, 16)
	audioData := make([]byte, 100) // 50 samples of int16

	// ds64 chunk:
	// 8 bytes riffSize, 8 bytes dataSize, 8 bytes sampleCount, 4 bytes tableLength
	var dsData [28]byte
	binary.LittleEndian.PutUint64(dsData[0:8], 1000)
	binary.LittleEndian.PutUint64(dsData[8:16], 100) // data size = 100 bytes
	binary.LittleEndian.PutUint64(dsData[16:24], 50) // sample count = 50
	binary.LittleEndian.PutUint32(dsData[24:28], 0)  // table length = 0

	wavBytes := buildWAVBytes("RF64", []struct {
		id   string
		data []byte
	}{
		{"ds64", dsData[:]},
		{"fmt ", fmtData},
		{"data", audioData},
	})

	r, err := NewReader(bytes.NewReader(wavBytes))
	if err != nil {
		t.Fatalf("NewReader RF64 failed: %v", err)
	}

	dst := make([]int16, 100)
	n, err := r.ReadInt16PCM(dst)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadInt16PCM failed: %v", err)
	}
	if n != 50 {
		t.Errorf("expected 50 samples read from RF64, got %d", n)
	}
}

type wavChunk = struct {
	id   string
	data []byte
}

func TestBlockAlign(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}

	// 16-bit PCM keeps being accepted whatever blockAlign says: some writers leave it 0.
	f16 := buildFmtChunk(FormatPCM, 1, 48000, 16)
	binary.LittleEndian.PutUint16(f16[12:14], 0)
	r, err := NewReader(bytes.NewReader(buildWAVBytes("RIFF", []wavChunk{{"fmt ", f16}, {"data", pcm}})))
	if err != nil {
		t.Fatalf("16-bit with blockAlign 0 rejected: %v", err)
	}
	if got := readAll(t, r, 4); len(got) != 2 || got[1] != 2 {
		t.Fatalf("samples = %v", got)
	}

	// Wider formats are refused on a mismatch: the frame size would be guessed wrong.
	f24 := buildFmtChunk(FormatPCM, 1, 48000, 24)
	binary.LittleEndian.PutUint16(f24[12:14], 4) // 24-bit samples in 4-byte slots
	if _, err := NewReader(bytes.NewReader(buildWAVBytes("RIFF", []wavChunk{{"fmt ", f24}, {"data", pcm}}))); !errors.Is(err, ErrUnsupportedWAV) {
		t.Fatalf("24-bit with blockAlign 4: err = %v, want ErrUnsupportedWAV", err)
	}

	// channels*bytes wraps to 0 in uint16 arithmetic (16384 * 4 = 65536); blockAlign 0 must not match.
	wrap := buildFmtChunk(FormatPCM, 16384, 48000, 32)
	binary.LittleEndian.PutUint16(wrap[12:14], 0)
	if _, err := NewReader(bytes.NewReader(buildWAVBytes("RIFF", []wavChunk{{"fmt ", wrap}, {"data", pcm}}))); !errors.Is(err, ErrUnsupportedWAV) {
		t.Fatalf("wrapped blockAlign: err = %v, want ErrUnsupportedWAV", err)
	}
}

func ds64Chunk(dataSize uint64) []byte {
	b := make([]byte, 28)
	binary.LittleEndian.PutUint64(b[8:16], dataSize)
	return b
}

// With ds64 present the 32-bit data size still wins unless it is the 0xFFFFFFFF marker.
func TestRF64DataSizeSource(t *testing.T) {
	audio := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	fmtData := buildFmtChunk(FormatPCM, 1, 48000, 16)

	// Real 32-bit size (4 bytes = 2 samples) next to a larger ds64 value: the chunk size wins.
	raw := buildWAVBytes("RF64", []wavChunk{{"ds64", ds64Chunk(1 << 40)}, {"fmt ", fmtData}, {"data", audio[:4]}})
	r, err := NewReader(bytes.NewReader(append(raw, audio[4:]...)))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 8); len(got) != 2 {
		t.Fatalf("read %d samples, want 2 (size from the data chunk)", len(got))
	}

	// 0xFFFFFFFF defers to ds64 (here 8 bytes = 4 samples).
	raw = buildWAVBytes("RF64", []wavChunk{{"ds64", ds64Chunk(8)}, {"fmt ", fmtData}, {"data", audio}})
	binary.LittleEndian.PutUint32(raw[len(raw)-len(audio)-4:], 0xFFFFFFFF)
	r, err = NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 8); len(got) != 4 {
		t.Fatalf("read %d samples, want 4 (size from ds64)", len(got))
	}
}

// A hostile ds64 size must not wrap when narrowed to int and end the read early.
func TestRF64HugeDataSize(t *testing.T) {
	audio := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	raw := buildWAVBytes("RF64", []wavChunk{{"ds64", ds64Chunk(1 << 63)}, {"fmt ", buildFmtChunk(FormatPCM, 1, 48000, 16)}, {"data", audio}})
	binary.LittleEndian.PutUint32(raw[len(raw)-len(audio)-4:], 0xFFFFFFFF)
	r, err := NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r, 8); len(got) != 4 {
		t.Fatalf("read %d samples, want all 4 that exist", len(got))
	}
}

func TestReadFloat32ClampsInfinity(t *testing.T) {
	vals := []float32{float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN()), 0.5, 2.5}
	var data []byte
	for _, v := range vals {
		data = binary.LittleEndian.AppendUint32(data, math.Float32bits(v))
	}
	r, err := NewReader(bytes.NewReader(buildWAVBytes("RIFF", []wavChunk{{"fmt ", buildFmtChunk(FormatIEEEFloat, 1, 48000, 32)}, {"data", data}})))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, 8)
	n, _ := r.ReadFloat32PCM(out)
	want := []float32{1, -1, 0, 0.5, 2.5} // headroom above 1.0 survives; non-finite values do not
	if n != len(want) {
		t.Fatalf("n = %d", n)
	}
	for i, w := range want {
		if out[i] != w {
			t.Errorf("sample %d = %v, want %v", i, out[i], w)
		}
	}
}
