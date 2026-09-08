package player

import (
	"bytes"
	"errors"
	_ "fmt"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/selawe/go-opus-codec/ogg"
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
		player.Seek(int64(position), io.SeekStart)

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
		io.Copy(io.Discard, player)
	}
}

func BenchmarkDecodeFloat32(bench *testing.B) {
	player, err := NewPlayerF32FromFile(testFilePath, true)
	if err != nil {
		bench.Fatalf("Failed to create player: %v", err)
	}

	bench.ResetTimer()
	for bench.Loop() {
		io.Copy(io.Discard, player)
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


