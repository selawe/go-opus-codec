package player

import (
	"bytes"
	"errors"
	_ "fmt"
	"io"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
)

const testFilePath = "../test/music_64kbps.opus"

func TestBasic(test *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create player: %v", err)
	}

	if player.CurrentSample() != 0 {
		test.Fatalf("Expected current sample to be 0, got %d", player.CurrentSample())
	}

	var buffer bytes.Buffer
	n, err := io.Copy(&buffer, player)
	if err != nil {
		test.Fatalf("Failed to read from player: %v", err)
	}

	if n != player.Length() {
		test.Fatalf("Expected to read %d bytes, got %d", player.Length(), n)
	}
}

func TestBasicF32(test *testing.T) {
	player, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create player: %v", err)
	}

	if player.CurrentSample() != 0 {
		test.Fatalf("Expected current sample to be 0, got %d", player.CurrentSample())
	}

	var buffer bytes.Buffer
	n, err := io.Copy(&buffer, player)
	if err != nil {
		test.Fatalf("Failed to read from player: %v", err)
	}

	if n != player.Length() {
		test.Fatalf("Expected to read %d bytes, got %d", player.Length(), n)
	}
}

func TestSameSamples(test *testing.T) {
	player1, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create first player: %v", err)
	}

	player2, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create second player: %v", err)
	}

	total1, err := player1.TotalSamples()
	if err != nil {
		test.Fatalf("Failed to get total samples from first player: %v", err)
	}

	total2, err := player2.TotalSamples()
	if err != nil {
		test.Fatalf("Failed to get total samples from second player: %v", err)
	}

	if total1 != total2 {
		test.Fatalf("Expected total samples to be the same, got %d and %d", total1, total2)
	}
}

func absTime(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestSeek1(test *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create player: %v", err)
	}

	if player.CurrentSample() != 0 {
		test.Fatalf("Expected current sample to be 0, got %d", player.CurrentSample())
	}

	err = player.SeekSample(1)
	if err != nil {
		test.Fatalf("Failed to seek to sample 1: %v", err)
	}

	if player.CurrentSample() != 1 {
		test.Fatalf("Expected current sample to be 1 after seeking, got %d", player.CurrentSample())
	}

	err = player.SeekTime(1 * time.Second)
	if err != nil {
		test.Fatalf("Failed to seek to 1 second: %v", err)
	}

	if player.CurrentSample() != int64(player.SampleRate()) {
		test.Fatalf("Expected current sample to be %d after seeking to 1 second, got %d", player.SampleRate(), player.CurrentSample())
	}

	if absTime(player.CurrentTime()-1*time.Second) > 1*time.Millisecond {
		test.Fatalf("Expected current time to be approximately 1 second after seeking, got %v", player.CurrentTime())
	}

	err = player.SeekSample(1 << 40)
	if err == nil {
		test.Fatalf("Expected error when seeking beyond file length, but got none")
	}
}

// this test decodes the entire stream into memory, then seeks to a position and reads data.
// the read data should exactly equal the corresponding data in the full stream at the same position
func TestSeek2(test *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create player: %v", err)
	}

	player2, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create second player: %v", err)
	}
	// decode the entire stream into memory
	var fullStream bytes.Buffer
	n, err := io.Copy(&fullStream, player2)
	if err != nil {
		test.Fatalf("Failed to decode full stream: %v", err)
	}

	if n != player.Length() {
		test.Fatalf("Expected full stream length %d, got %d", player.Length(), n)
	}

	position := int64(5000 * 4)
	where, err := player.Seek(position, io.SeekStart)
	if err != nil {
		test.Fatalf("Failed to seek to position %d: %v", position, err)
	}

	if where != position {
		test.Fatalf("Expected seek position %d, got %d", position, where)
	}

	decoded := make([]byte, 800*4)
	decodedLength, err := player.Read(decoded)
	if err != nil {
		test.Fatalf("Failed to read after seeking: %v", err)
	}

	if decodedLength != len(decoded) {
		test.Fatalf("Expected to read %d bytes, got %d", len(decoded), decodedLength)
	}

	checkBytes := func(decoded []byte, position int64) {
		index := bytes.Index(fullStream.Bytes(), decoded)
		if index != int(position) {

			b := fullStream.Bytes()[position:]

			if len(b) < len(decoded) {
				test.Fatalf("Decoded data length mismatch after seeking, expected %d, got %d", len(b), len(decoded))
			}

			for i := range decoded {
				if decoded[i] != b[i] {
					test.Fatalf("%d: Data mismatch at byte %d after seeking, expected %02x, got %02x", position, i, b[i], decoded[i])
				}
			}

			test.Fatalf("Decoded data does not match expected data after seeking, expected index %d, got %d", position, index)
		}
	}

	checkBytes(decoded, position)

	for i := range 6 {
		position := int(math.Pow(7, float64(i+1))) * 4
		if _, err := player.Seek(int64(position), io.SeekStart); err != nil {
			test.Fatalf("Failed to seek to position %d: %v", position, err)
		}

		decodedLength, err := player.Read(decoded)
		if err != nil {
			test.Fatalf("Failed to read after seeking to position %d: %v", position, err)
		}
		if decodedLength != len(decoded) {
			test.Fatalf("Expected to read %d bytes after seeking to position %d, got %d", len(decoded), position, decodedLength)
		}

		checkBytes(decoded, int64(position))
	}

	// this is very near the end of the stream where the last granule should drop
	// some excess samples
	position = int64(4358531-350) * 4
	where, err = player.Seek(position, io.SeekStart)
	if err != nil {
		test.Fatalf("Failed to seek to position %d: %v", position, err)
	}

	if where != position {
		test.Fatalf("Expected seek position %d, got %d", position, where)
	}

	decoded = make([]byte, 10000)

	decodedLength, err = player.Read(decoded)
	if err != nil && !errors.Is(err, io.EOF) {
		test.Fatalf("Failed to read after seeking: %v", err)
	}
	// fmt.Printf("Read %d bytes after seeking to position %d\n", decodedLength, position)
	checkBytes(decoded[:decodedLength], position)
}

func TestSeekEnd(test *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		test.Fatalf("Failed to create player: %v", err)
	}

	totalSamples := player.Length() / 2 / int64(player.Channels())

	position, err := player.Seek(-4, io.SeekEnd)
	if err != nil {
		test.Fatalf("Failed to seek to end - 4 bytes: %v", err)
	}

	if position != (totalSamples-1)*4 {
		test.Fatalf("Expected seek position to be %d, got %d", totalSamples-1, position)
	}
}

func BenchmarkSeek(bench *testing.B) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		bench.Fatalf("Failed to create player: %v", err)
	}

	bench.ResetTimer()
	for bench.Loop() {
		_, err := player.Seek(500000, io.SeekStart)
		if err != nil {
			bench.Fatalf("Failed to seek during benchmark: %v", err)
		}
	}
}

func BenchmarkDecodeInt16(bench *testing.B) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		bench.Fatalf("Failed to create player: %v", err)
	}

	bench.ResetTimer()
	for bench.Loop() {
		if _, err := io.Copy(io.Discard, player); err != nil {
			bench.Fatalf("Failed to decode during benchmark: %v", err)
		}
	}
}

func BenchmarkDecodeFloat32(bench *testing.B) {
	player, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		bench.Fatalf("Failed to create player: %v", err)
	}

	bench.ResetTimer()
	for bench.Loop() {
		if _, err := io.Copy(io.Discard, player); err != nil {
			bench.Fatalf("Failed to decode during benchmark: %v", err)
		}
	}
}

func TestMultichannelPlayer(t *testing.T) {
	head := ogg.OpusHead{
		Version:              1,
		Channels:             6, // 5.1 surround
		PreSkip:              312,
		InputSampleRate:      48000,
		OutputGainQ8:         0,
		ChannelMappingFamily: 1,
		StreamCount:          4,
		CoupledStreamCount:   2,
		ChannelMapping:       []byte{0, 4, 1, 2, 3, 5},
	}

	tags := ogg.OpusTags{
		Vendor: "test-multichannel",
	}

	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatalf("head.ToPacket: %v", err)
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		t.Fatalf("tags.ToPacket: %v", err)
	}

	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, 0x12345678)
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("write tags: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("pw.Flush: %v", err)
	}

	// Create a player from this 5.1 stream
	p, err := NewPlayerFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewPlayerFromReader: %v", err)
	}

	// Verify channels
	if p.Channels() != 6 {
		t.Fatalf("expected 6 channels for 5.1 player, got %d", p.Channels())
	}
}

func TestPlayer_ConcurrentAccess(t *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, false) // in-memory seekable
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer player.Close()

	var wg sync.WaitGroup
	// 4 concurrent goroutines reading and querying player state
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := make([]byte, 1024)
			for j := 0; j < 50; j++ {
				_, _ = player.Read(buf)
				_ = player.CurrentSample()
				_ = player.CurrentTime()
				_ = player.IsFinished()
				if j%10 == 0 {
					_ = player.SeekSample(uint64(j * 100))
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestPlayer_Close(t *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}

	buf := make([]byte, 512)
	n, err := player.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("expected to read before close, n=%d, err=%v", n, err)
	}

	// Close the player
	if err := player.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Multiple calls to Close should be safe
	if err := player.Close(); err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}

	// Read after Close should return ErrClosed
	_, err = player.Read(buf)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on Read after Close, got: %v", err)
	}

	// Seek after Close should return ErrClosed
	_, err = player.Seek(0, io.SeekStart)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on Seek after Close, got: %v", err)
	}
}

func TestPlayer_ShortAndUnalignedReads(t *testing.T) {
	// 1. Test int16 player with unaligned/short slice sizes (must never hang)
	playerI16, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer playerI16.Close()

	// Short slices smaller than 1 frame (stereo int16 = 4 bytes)
	for _, sz := range []int{1, 2, 3} {
		tiny := make([]byte, sz)
		n, err := playerI16.Read(tiny)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Read(len=%d) error: %v", sz, err)
		}
		if n != 0 {
			t.Fatalf("expected 0 bytes for sub-frame read of %d bytes, got %d", sz, n)
		}
	}

	// Slices not aligned to frame size (e.g. 5, 7, 9 bytes)
	for _, sz := range []int{5, 7, 9} {
		buf := make([]byte, sz)
		n, err := playerI16.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Read(len=%d) error: %v", sz, err)
		}
		if n%4 != 0 {
			t.Fatalf("expected read byte count to be multiple of 4, got %d for buffer size %d", n, sz)
		}
	}

	// 2. Test float32 player with unaligned/short slice sizes (stereo float32 = 8 bytes)
	playerF32, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerF32FromFile: %v", err)
	}
	defer playerF32.Close()

	for _, sz := range []int{1, 3, 5, 7} {
		tiny := make([]byte, sz)
		n, err := playerF32.Read(tiny)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("F32 Read(len=%d) error: %v", sz, err)
		}
		if n != 0 {
			t.Fatalf("expected 0 bytes for sub-frame float32 read of %d bytes, got %d", sz, n)
		}
	}

	for _, sz := range []int{9, 13, 17} {
		buf := make([]byte, sz)
		n, err := playerF32.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("F32 Read(len=%d) error: %v", sz, err)
		}
		if n%8 != 0 {
			t.Fatalf("expected read byte count to be multiple of 8, got %d for buffer size %d", n, sz)
		}
	}
}

func TestPlayer_TimestampSubsecondPrecision(t *testing.T) {
	player, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer player.Close()

	// Update timestamp with 24000 samples (0.5 seconds = 500ms).
	// Previously, 24000 / 48000 * 1s truncated to 0s!
	player.updateTimestamp(24000)
	ts := player.CurrentStreamTimestamp()
	if ts != 500*time.Millisecond {
		t.Fatalf("expected 500ms, got %v", ts)
	}

	player.updateTimestamp(960) // 20ms
	ts = player.CurrentStreamTimestamp()
	if ts != 20*time.Millisecond {
		t.Fatalf("expected 20ms, got %v", ts)
	}
}

func TestPlayer_VolumeDefaultAndClamping(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p.Close()

	if v := p.Volume(); v != 1.0 {
		t.Errorf("expected default volume 1.0, got %f", v)
	}
	if g := p.Gain(); math.Abs(g) > 1e-6 {
		t.Errorf("expected default gain ~0 dB, got %f", g)
	}

	p.SetVolume(-0.5)
	if v := p.Volume(); v != 0.0 {
		t.Errorf("expected clamped volume 0.0 for negative, got %f", v)
	}
	if g := p.Gain(); !math.IsInf(g, -1) {
		t.Errorf("expected -inf gain for volume 0.0, got %f", g)
	}

	p.SetGain(-6.0)
	if v := p.Volume(); math.Abs(v-0.501187) > 0.001 {
		t.Errorf("expected volume ~0.501 for -6 dB, got %f", v)
	}

	p.SetGain(6.0)
	if v := p.Volume(); math.Abs(v-1.99526) > 0.001 {
		t.Errorf("expected volume ~1.995 for +6 dB, got %f", v)
	}

	p.SetGain(-150.0)
	if v := p.Volume(); v != 0.0 {
		t.Errorf("expected volume 0.0 for -150 dB, got %f", v)
	}
}

func TestPlayer_SetVolume_MuteInt16AndFloat32(t *testing.T) {
	// Test int16 mute
	p16, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p16.Close()

	p16.SetVolume(0.0)
	buf16 := make([]byte, 4096)
	n, err := p16.Read(buf16)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read int16: %v", err)
	}
	if n == 0 {
		t.Fatal("expected to read audio bytes, got 0")
	}
	for i := 0; i < n; i++ {
		if buf16[i] != 0 {
			t.Fatalf("expected muted byte at index %d to be 0, got %d", i, buf16[i])
		}
	}

	// Test float32 mute
	pF32, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerF32FromFile: %v", err)
	}
	defer pF32.Close()

	pF32.SetVolume(0.0)
	bufF32 := make([]byte, 4096)
	n, err = pF32.Read(bufF32)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read float32: %v", err)
	}
	if n == 0 {
		t.Fatal("expected to read audio bytes, got 0")
	}
	for i := 0; i < n; i++ {
		if bufF32[i] != 0 {
			t.Fatalf("expected muted byte at index %d to be 0, got %d", i, bufF32[i])
		}
	}
}

func TestPlayer_SetVolume_ScaleInt16(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p.Close()

	rawBuf := make([]byte, 4096)
	n1, err := p.Read(rawBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read normal: %v", err)
	}

	// Seek back to start
	if _, err := p.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	// Set half volume
	p.SetVolume(0.5)
	scaledBuf := make([]byte, 4096)
	n2, err := p.Read(scaledBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read scaled: %v", err)
	}

	if n1 != n2 {
		t.Fatalf("byte counts differ: normal=%d scaled=%d", n1, n2)
	}

	checked := 0
	for i := 0; i < n1/2; i++ {
		orig := int16(uint16(rawBuf[i*2]) | (uint16(rawBuf[i*2+1]) << 8))
		scaled := int16(uint16(scaledBuf[i*2]) | (uint16(scaledBuf[i*2+1]) << 8))

		expected := int16(math.Round(float64(orig) * 0.5))
		diff := int(scaled) - int(expected)
		if diff < -1 || diff > 1 {
			t.Fatalf("sample %d scaled incorrectly: orig=%d scaled=%d expected=%d diff=%d",
				i, orig, scaled, expected, diff)
		}
		if orig != 0 {
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no non-zero samples checked")
	}
}

func TestPlayer_SetVolume_ScaleFloat32(t *testing.T) {
	p, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerF32FromFile: %v", err)
	}
	defer p.Close()

	rawBuf := make([]byte, 4096)
	n1, err := p.Read(rawBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read normal: %v", err)
	}

	if _, err := p.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	p.SetVolume(0.5)
	scaledBuf := make([]byte, 4096)
	n2, err := p.Read(scaledBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read scaled: %v", err)
	}

	if n1 != n2 {
		t.Fatalf("byte counts differ: normal=%d scaled=%d", n1, n2)
	}

	checked := 0
	for i := 0; i < n1/4; i++ {
		uOrig := uint32(rawBuf[i*4]) | (uint32(rawBuf[i*4+1]) << 8) | (uint32(rawBuf[i*4+2]) << 16) | (uint32(rawBuf[i*4+3]) << 24)
		uScaled := uint32(scaledBuf[i*4]) | (uint32(scaledBuf[i*4+1]) << 8) | (uint32(scaledBuf[i*4+2]) << 16) | (uint32(scaledBuf[i*4+3]) << 24)

		orig := math.Float32frombits(uOrig)
		scaled := math.Float32frombits(uScaled)

		expected := orig * 0.5
		if math.Abs(float64(scaled-expected)) > 1e-6 {
			t.Fatalf("sample %d float scaled incorrectly: orig=%f scaled=%f expected=%f",
				i, orig, scaled, expected)
		}
		if orig != 0 {
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no non-zero float samples checked")
	}
}

func TestPlayer_VolumeConcurrent(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for i := 0; i < 50; i++ {
			_, err := p.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	for i := 0; i < 50; i++ {
		p.SetVolume(float64(i) / 50.0)
		_ = p.Volume()
		p.SetGain(float64(i%12) - 6.0)
		_ = p.Gain()
	}
	<-done
}

func TestPlayer_SeekNegative(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p.Close()

	if _, err := p.Seek(-10, io.SeekStart); err == nil {
		t.Error("expected error seeking to negative offset with SeekStart, got nil")
	}

	if err := p.SeekTime(-time.Second); err == nil {
		t.Error("expected error for negative SeekTime, got nil")
	}
}

func TestPlayer_SeekResetsFinished(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer p.Close()

	// Drain player completely
	var buf bytes.Buffer
	_, err = io.Copy(&buf, p)
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}

	if !p.finished {
		t.Fatal("expected player to be finished after reading to EOF")
	}

	// Seek back to start
	if _, err := p.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek to start: %v", err)
	}

	if p.finished {
		t.Fatal("player.finished should be reset to false after seek")
	}

	// Read should succeed now
	scratch := make([]byte, 1024)
	n, err := p.Read(scratch)
	if err != nil {
		t.Fatalf("Read after seek to start failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected to read > 0 bytes after seek to start")
	}
}

func TestPlayer_FirstPacketPreSkip(t *testing.T) {
	enc, err := opus.NewEncoder(48000, 1, opus.ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	pcm1 := make([]int16, 960)
	for i := range pcm1 {
		pcm1[i] = int16(i * 10)
	}
	pkt1 := make([]byte, 1000)
	n1, err := enc.Encode(pcm1, 960, pkt1)
	if err != nil {
		t.Fatal(err)
	}
	pkt1 = pkt1[:n1]

	pcm2 := make([]int16, 960)
	for i := range pcm2 {
		pcm2[i] = int16(i * 10)
	}
	pkt2 := make([]byte, 1000)
	n2, err := enc.Encode(pcm2, 960, pkt2)
	if err != nil {
		t.Fatal(err)
	}
	pkt2 = pkt2[:n2]

	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, 0x12345678)
	head := ogg.OpusHead{
		Version:         1,
		Channels:        1,
		PreSkip:         312,
		InputSampleRate: 48000,
	}
	headBytes, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket(headBytes, 0, true, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}

	tagsBytes, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket(tagsBytes, 0, false, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}

	// Packet 1: GranulePos 960
	if err := pw.WritePacket(pkt1, 960, false, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}

	// Packet 2: GranulePos 1920 (EOS)
	if err := pw.WritePacket(pkt2, 1920, false, true); err != nil {
		t.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}

	p, err := NewPlayerFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var out bytes.Buffer
	n, err := io.Copy(&out, p)
	if err != nil {
		t.Fatal(err)
	}

	// Expected: 1920 - 312 = 1608 samples. Mono expanded to stereo (2 ch * 2 bytes = 4 bytes/sample)
	// 1608 * 4 = 6432 bytes.
	const wantBytes = 1608 * 4
	if n != wantBytes {
		t.Fatalf("first packet wrongly trimmed: got %d bytes (%d samples), want %d bytes (%d samples)",
			n, n/4, wantBytes, wantBytes/4)
	}
}

func TestPlayerCoverage(t *testing.T) {
	head := ogg.OpusHead{
		Version:         1,
		Channels:        2,
		PreSkip:         312,
		InputSampleRate: 48000,
	}
	tags := ogg.OpusTags{Vendor: "test"}
	headPkt, _ := ogg.BuildOpusHeadPacket(head)
	tagsPkt, _ := ogg.BuildOpusTagsPacket(tags)

	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, 0x1234)
	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		t.Fatalf("write tags: %v", err)
	}
	if err := pw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	r := bytes.NewReader(buf.Bytes())

	// Test NewPlayerF32FromReader
	p, err := NewPlayerF32FromReader(r)
	if err != nil {
		t.Fatalf("NewPlayerF32FromReader: %v", err)
	}

	// Test TotalDuration
	dur, err := p.TotalDuration()
	if err != nil {
		t.Fatalf("TotalDuration err: %v", err)
	}
	if dur != 0 {
		t.Fatalf("Expected duration 0, got %v", dur)
	}

	// Test ReadPacket
	outBuf := make([]byte, 1024)
	n, err := p.ReadPacket(outBuf)
	if !errors.Is(err, io.EOF) { // no audio packets, expects EOF
		t.Fatalf("ReadPacket expected EOF, got err=%v n=%d", err, n)
	}
}

// Length and TotalSamples are documented as safe to call at any time; calling
// them mid-playback must not truncate the rest of the stream.
func TestPlayer_LengthDoesNotDisturbPlayback(t *testing.T) {
	for _, stream := range []bool{true, false} {
		p, err := NewPlayerFromFile(testFilePath, stream)
		if err != nil {
			t.Fatalf("stream=%v: NewPlayerFromFile: %v", stream, err)
		}

		head := make([]byte, 200000)
		if _, err := io.ReadFull(p, head); err != nil {
			t.Fatalf("stream=%v: initial read: %v", stream, err)
		}
		length := p.Length()
		if _, err := p.TotalSamples(); err != nil {
			t.Fatalf("stream=%v: TotalSamples: %v", stream, err)
		}
		if _, err := p.TotalDuration(); err != nil {
			t.Fatalf("stream=%v: TotalDuration: %v", stream, err)
		}

		rest, err := io.Copy(io.Discard, p)
		if err != nil {
			t.Fatalf("stream=%v: read rest: %v", stream, err)
		}
		if got := int64(len(head)) + rest; got != length {
			t.Errorf("stream=%v: read %d bytes in total, Length() = %d", stream, got, length)
		}
		p.Close()
	}
}

// A final granule smaller than the pre-skip (truncated or hostile stream) means
// there is no audible output; it must end cleanly instead of panicking.
func TestPlayer_GranuleBelowPreSkip(t *testing.T) {
	for _, ch := range []int{1, 2} {
		for _, f32 := range []bool{false, true} {
			testGranuleBelowPreSkip(t, ch, f32)
		}
	}
}

func testGranuleBelowPreSkip(t *testing.T, channels int, f32 bool) {
	t.Helper()
	enc, err := opus.NewEncoder(48000, channels, opus.ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	pkt := make([]byte, 1000)
	n, err := enc.Encode(make([]int16, 960*channels), 960, pkt)
	if err != nil {
		t.Fatal(err)
	}

	{
		var buf bytes.Buffer
		pw := ogg.NewPacketWriter(&buf, 0x12345678)
		headBytes, err := ogg.BuildOpusHeadPacket(ogg.OpusHead{Version: 1, Channels: uint8(channels), PreSkip: 312, InputSampleRate: 48000})
		if err != nil {
			t.Fatal(err)
		}
		tagsBytes, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "test"})
		if err != nil {
			t.Fatal(err)
		}
		for i, pk := range [][]byte{headBytes, tagsBytes} {
			if err := pw.WritePacket(pk, 0, i == 0, false); err != nil {
				t.Fatal(err)
			}
			if err := pw.Flush(); err != nil {
				t.Fatal(err)
			}
		}
		if err := pw.WritePacket(pkt[:n], 100, false, true); err != nil {
			t.Fatal(err)
		}
		if err := pw.Flush(); err != nil {
			t.Fatal(err)
		}

		var got int64
		if f32 {
			p, err := NewPlayerF32FromReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			got, err = io.Copy(io.Discard, p)
			if err != nil {
				t.Fatalf("ch=%d f32: %v", channels, err)
			}
			p.Close()
		} else {
			p, err := NewPlayerFromReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			got, err = io.Copy(io.Discard, p)
			if err != nil {
				t.Fatalf("ch=%d int16: %v", channels, err)
			}
			p.Close()
		}
		if got != 0 {
			t.Errorf("ch=%d f32=%v: got %d bytes, want 0", channels, f32, got)
		}
	}
}

// Seek(0, io.SeekCurrent) is the conventional way to query the position; it
// must not re-seek or reset the decoder, or the audio after it would differ.
func TestPlayer_SeekCurrentZeroIsPure(t *testing.T) {
	ref, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	want, err := io.ReadAll(ref)
	if err != nil {
		t.Fatal(err)
	}

	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	const head = 300000
	got := make([]byte, head)
	if _, err := io.ReadFull(p, got); err != nil {
		t.Fatal(err)
	}
	pos, err := p.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if pos != head {
		t.Errorf("Seek(0, SeekCurrent) = %d, want %d", pos, head)
	}
	rest, err := io.ReadAll(p)
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, rest...)
	if !bytes.Equal(got, want) {
		t.Errorf("audio after Seek(0, SeekCurrent) differs from uninterrupted playback (got %d bytes, want %d)", len(got), len(want))
	}
}

// RFC 7845 Section 4.6: after a seek the decoder must run at least 80 ms before
// the target so its state converges. Audio right after SeekSample must match
// uninterrupted playback.
func TestPlayer_SeekHasPreRoll(t *testing.T) {
	ref, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	want, err := io.ReadAll(ref)
	if err != nil {
		t.Fatal(err)
	}

	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	const window = 4000 * 4 // 4000 stereo int16 frames
	for _, pos := range []uint64{47999, 96000, 250001, 700000} {
		if err := p.SeekSample(pos); err != nil {
			t.Fatalf("SeekSample(%d): %v", pos, err)
		}
		got := make([]byte, window)
		if _, err := io.ReadFull(p, got); err != nil {
			t.Fatalf("read after seek to %d: %v", pos, err)
		}
		exp := want[pos*4 : pos*4+window]
		var sum float64
		for i := 0; i+1 < window; i += 2 {
			a := int16(uint16(got[i]) | uint16(got[i+1])<<8)
			b := int16(uint16(exp[i]) | uint16(exp[i+1])<<8)
			d := int(a) - int(b)
			if d < 0 {
				d = -d
			}
			sum += float64(d)
		}
		if mean := sum / (window / 2); mean > 2 {
			t.Errorf("seek to %d: mean abs error vs continuous playback = %.1f LSB (no pre-roll?)", pos, mean)
		}
	}
}

type failingReader struct {
	r   io.Reader
	n   int
	err error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	if len(p) > f.n {
		p = p[:f.n]
	}
	n, err := f.r.Read(p)
	f.n -= n
	return n, err
}

// A stream cut short by an I/O error must keep reporting that error, not turn into a
// clean io.EOF on the next read.
func TestPlayer_ReadErrorIsSticky(t *testing.T) {
	data, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	p, err := NewPlayerFromReader(&failingReader{r: bytes.NewReader(data), n: 30000, err: boom})
	if err != nil {
		t.Fatalf("NewPlayerFromReader: %v", err)
	}
	defer p.Close()

	_, err = io.Copy(io.Discard, p)
	if !errors.Is(err, boom) {
		t.Fatalf("first error = %v, want %v", err, boom)
	}
	buf := make([]byte, 4096)
	for i := range 3 {
		if _, err := p.Read(buf); !errors.Is(err, boom) {
			t.Fatalf("read %d after failure: err = %v, want %v", i, err, boom)
		}
	}
}

func TestPlayer_SetVolumeIgnoresNonFinite(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.SetVolume(0.5)
	p.SetVolume(math.NaN())
	p.SetVolume(math.Inf(1))
	p.SetGain(math.Inf(1))
	if v := p.Volume(); v != 0.5 {
		t.Fatalf("volume = %v, want 0.5 unchanged", v)
	}
}

func TestSampleDurationConversionsDoNotOverflow(t *testing.T) {
	if got := samplesToDuration(48000); got != time.Second {
		t.Fatalf("48000 samples = %v, want 1s", got)
	}
	if got := samplesToDuration(1); got != time.Second/48000 {
		t.Fatalf("1 sample = %v", got)
	}
	// 100 hours of audio used to wrap negative with samples*time.Second.
	const hundredHours = int64(100 * 3600 * 48000)
	if got := samplesToDuration(hundredHours); got != 100*time.Hour {
		t.Fatalf("100h = %v", got)
	}
	if got := samplesToDuration(math.MaxInt64); got <= 0 {
		t.Fatalf("huge sample count gave %v, want a saturated positive duration", got)
	}
	if got := durationToSamples(100 * time.Hour); got != uint64(hundredHours) {
		t.Fatalf("100h = %d samples", got)
	}
	if got := durationToSamples(-time.Second); got != 0 {
		t.Fatalf("negative = %d", got)
	}
}

// An absurd seek position is refused without wrapping, and playback keeps working.
func TestPlayer_SeekOutOfRangeLeavesPlaybackIntact(t *testing.T) {
	p, err := NewPlayerFromFile(testFilePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	buf := make([]byte, 4800)
	if _, err := io.ReadFull(p, buf); err != nil {
		t.Fatal(err)
	}
	before := p.CurrentSample()

	if err := p.SeekSample(math.MaxUint64 - 10); err == nil {
		t.Fatal("expected an out-of-range error")
	}
	if got := p.CurrentSample(); got != before {
		t.Fatalf("position moved from %d to %d after a refused seek", before, got)
	}
	if _, err := io.ReadFull(p, buf); err != nil {
		t.Fatalf("playback broken after refused seek: %v", err)
	}
	if err := p.SeekTime(time.Duration(math.MaxInt64)); err != nil {
		t.Logf("SeekTime(max) -> %v (acceptable)", err)
	}
}

// encodeChainPart returns one complete Ogg Opus stream of a sine wave: frames 20 ms frames, whose end-of-stream
// granule trims 100 samples off the last one, so the pre-skip and end-trimming of every part are exercised.
func encodeChainPart(t *testing.T, serial uint32, channels, frames int, freq float64) []byte {
	t.Helper()
	enc, err := opus.NewEncoder(48000, channels, opus.ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	preSkip, err := enc.PreSkip()
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, serial)
	pw.MaxPageSize = 8000
	if err := pw.WriteOpusHeaders(ogg.OpusHead{Version: 1, Channels: uint8(channels), PreSkip: uint16(preSkip), InputSampleRate: 48000}, ogg.OpusTags{Vendor: "chain"}); err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, 960*channels)
	pkt := make([]byte, 4000)
	for f := 0; f < frames; f++ {
		for i := 0; i < 960; i++ {
			v := int16(9000 * math.Sin(2*math.Pi*freq*float64(f*960+i)/48000))
			for c := 0; c < channels; c++ {
				pcm[i*channels+c] = v
			}
		}
		n, err := enc.Encode(pcm, 960, pkt)
		if err != nil {
			t.Fatal(err)
		}
		granule := uint64(preSkip) + uint64(f+1)*960
		last := f == frames-1
		if last {
			granule -= 100
		}
		if err := pw.WritePacket(pkt[:n], granule, false, last); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func playAll(t *testing.T, data []byte) []byte {
	t.Helper()
	p, err := NewPlayerFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	out, err := io.ReadAll(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A chained file plays through every stream, each with its own pre-skip, end trimming and a fresh decoder:
// the output is exactly the parts played one after another.
func TestPlayer_FollowsChainedStreams(t *testing.T) {
	a := encodeChainPart(t, 1, 2, 12, 300)
	b := encodeChainPart(t, 2, 2, 9, 1700)
	c := encodeChainPart(t, 3, 2, 5, 600)
	wantA, wantB, wantC := playAll(t, a), playAll(t, b), playAll(t, c)
	want := append(append(append([]byte(nil), wantA...), wantB...), wantC...)

	p, err := NewPlayerFromReader(bytes.NewReader(append(append(append([]byte(nil), a...), b...), c...)))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// Reading exactly the first stream must not report the end: another stream follows.
	first := make([]byte, len(wantA))
	if _, err := io.ReadFull(p, first); err != nil {
		t.Fatal(err)
	}
	if p.IsFinished() {
		t.Fatal("IsFinished is true after the first stream although two more follow")
	}
	rest, err := io.ReadAll(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := append(first, rest...); !bytes.Equal(got, want) {
		t.Fatalf("chained playback differs from the parts played separately (%d vs %d bytes)", len(got), len(want))
	}
	if !p.IsFinished() {
		t.Error("IsFinished is false after the last stream")
	}
	if got, wantSamples := p.CurrentSample(), int64(len(want)/4); got != wantSamples {
		t.Errorf("CurrentSample = %d, want %d", got, wantSamples)
	}
}

// The output format is fixed, so a chained stream with another channel count ends playback with an error that
// stays: the earlier stream is played in full first.
func TestPlayer_ChainedChannelChangeIsAStickyError(t *testing.T) {
	a := encodeChainPart(t, 1, 2, 6, 300)
	b := encodeChainPart(t, 2, 1, 6, 300)
	wantA := playAll(t, a)

	p, err := NewPlayerFromReader(bytes.NewReader(append(append([]byte(nil), a...), b...)))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	got, err := io.ReadAll(p)
	if !errors.Is(err, ErrStreamChange) {
		t.Fatalf("err = %v, want ErrStreamChange", err)
	}
	if !bytes.Equal(got, wantA) {
		t.Fatalf("the first stream was not played in full before the error (%d vs %d bytes)", len(got), len(wantA))
	}
	buf := make([]byte, 64)
	if _, err := p.Read(buf); !errors.Is(err, ErrStreamChange) {
		t.Fatalf("a later Read returned %v, want the same error", err)
	}
}

func splitRawPages(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var pages [][]byte
	pr := ogg.NewPageReader(bytes.NewReader(raw))
	off := 0
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			return pages
		}
		if err != nil {
			t.Fatal(err)
		}
		n := 27 + len(p.SegmentTable) + len(p.SegmentData)
		pages = append(pages, raw[off:off+n])
		off += n
	}
}

// muxStreams interleaves whole streams page by page, all BOS pages first (the Ogg grouping rule).
func muxStreams(t *testing.T, streams ...[]byte) []byte {
	t.Helper()
	pages := make([][][]byte, len(streams))
	var out bytes.Buffer
	for i, s := range streams {
		pages[i] = splitRawPages(t, s)
		out.Write(pages[i][0])
		pages[i] = pages[i][1:]
	}
	for more := true; more; {
		more = false
		for i := range pages {
			if len(pages[i]) > 0 {
				out.Write(pages[i][0])
				pages[i] = pages[i][1:]
				more = true
			}
		}
	}
	return out.Bytes()
}

func fakeForeignStream(t *testing.T, serial uint32, packets int) []byte {
	t.Helper()
	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, serial)
	if err := pw.WritePacket([]byte("\x01vorbis-identification"), 0, true, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.FlushPage(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < packets; i++ {
		if err := pw.WritePacket(bytes.Repeat([]byte{0xAA}, 300), uint64(i+1)*1024, false, i == packets-1); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// In a multiplexed file the player plays the first Opus stream and skips the rest; options choose another.
func TestPlayer_MultiplexedFile(t *testing.T) {
	english := encodeChainPart(t, 10, 2, 8, 300)
	bahasa := encodeChainPart(t, 20, 2, 6, 900)
	wantEnglish, wantBahasa := playAll(t, english), playAll(t, bahasa)
	file := muxStreams(t, fakeForeignStream(t, 5, 20), english, bahasa)

	if got := playAll(t, file); !bytes.Equal(got, wantEnglish) {
		t.Fatalf("default: played %d bytes that are not the first Opus stream (%d bytes)", len(got), len(wantEnglish))
	}

	p, err := NewPlayerFromReader(bytes.NewReader(file), ogg.WithSerial(20))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if got, err := io.ReadAll(p); err != nil || !bytes.Equal(got, wantBahasa) {
		t.Fatalf("WithSerial(20): %d bytes, err %v; want the second stream (%d bytes)", len(got), err, len(wantBahasa))
	}

	// Seeking works on the selected stream of a multiplexed file as it does on that stream alone.
	alone, err := NewPlayerFromReader(bytes.NewReader(bahasa))
	if err != nil {
		t.Fatal(err)
	}
	defer alone.Close()
	for _, p2 := range []*OpusPlayer[int16]{p, alone} {
		if err := p2.SeekSample(2000); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := io.ReadAll(p)
	b, _ := io.ReadAll(alone)
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Fatalf("after a seek the multiplexed player differs from the stream alone (%d vs %d bytes)", len(a), len(b))
	}
}
