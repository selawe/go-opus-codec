package ogg

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"testing"
)

// seekOutcome is what a seek leaves the reader in: the granule it reports and the packet read next.
type seekOutcome struct {
	last    uint64
	granule uint64
	size    int
	first8  [8]byte
	ok      bool
}

// referenceSeek defines the correct result by reading every packet from the start: the first packet
// whose page granule reaches target, and the granule of the last packet before it.
func referenceSeek(t *testing.T, data []byte, target uint64) seekOutcome {
	t.Helper()
	r := NewPacketReader(bytes.NewReader(data))
	r.SetMaxPacketSize(0)
	last := uint64(0)
	for {
		p, err := r.ReadPacket()
		if err != nil {
			return seekOutcome{}
		}
		if p.GranulePosition != math.MaxUint64 {
			if p.GranulePosition >= target {
				o := seekOutcome{last: last, granule: p.GranulePosition, size: len(p.Data), ok: true}
				copy(o.first8[:], p.Data)
				return o
			}
			last = p.GranulePosition
		}
	}
}

func actualSeek(t *testing.T, data []byte, target uint64) (seekOutcome, error) {
	t.Helper()
	r := NewPacketReader(bytes.NewReader(data))
	r.SetMaxPacketSize(0)
	// Read a few packets first, as a player would, so the seek starts from a used reader.
	for range 3 {
		if _, err := r.ReadPacket(); err != nil {
			break
		}
	}
	last, err := r.SeekToPage(target)
	if err != nil {
		return seekOutcome{}, err
	}
	p, err := r.ReadPacket()
	if err != nil {
		return seekOutcome{}, err
	}
	o := seekOutcome{last: last, granule: p.GranulePosition, size: len(p.Data), ok: true}
	copy(o.first8[:], p.Data)
	return o, nil
}

// spanningStream builds a stream of large packets, so pages carry granule -1 (a packet continues
// onto the next page) and page boundaries do not line up with packets.
func spanningStream(t *testing.T, n int, seed int64) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 77)
	head, _ := BuildOpusHeadPacket(OpusHead{Version: 1, Channels: 2, PreSkip: 312, InputSampleRate: 48000})
	tags, _ := BuildOpusTagsPacket(OpusTags{Vendor: "seek"})
	for i, p := range [][]byte{head, tags} {
		if err := pw.WritePacket(p, 0, i == 0, false); err != nil {
			t.Fatal(err)
		}
		if err := pw.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	granule := uint64(312)
	for i := 0; i < n; i++ {
		pkt := make([]byte, 20000+rng.Intn(50000))
		binary.LittleEndian.PutUint64(pkt, uint64(i)+1)
		granule += 960
		if err := pw.WritePacket(pkt, granule, false, i == n-1); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func compareSeeks(t *testing.T, name string, data []byte, targets []uint64) {
	t.Helper()
	for _, target := range targets {
		want := referenceSeek(t, data, target)
		got, err := actualSeek(t, data, target)
		switch {
		case !want.ok && err == nil:
			t.Errorf("%s target %d: want an error (beyond the end), got %+v", name, target, got)
		case want.ok && err != nil:
			t.Errorf("%s target %d: unexpected error %v, want %+v", name, target, err, want)
		case want.ok && got != want:
			t.Errorf("%s target %d: got %+v, want %+v", name, target, got, want)
		}
	}
}

func TestSeekToPageMatchesLinearReference(t *testing.T) {
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		var targets []uint64
		for g := uint64(1); g < 4_000_000; g += 37_771 {
			targets = append(targets, g)
		}
		targets = append(targets, 312, 313, 960, 48000, 5_000_000_000)
		compareSeeks(t, "music", data, targets)
	}

	data := spanningStream(t, 60, 1)
	var targets []uint64
	for g := uint64(1); g < 312+960*62; g += 523 {
		targets = append(targets, g)
	}
	for i := uint64(0); i < 62; i++ { // exact page/packet granules and their neighbours
		g := 312 + 960*i
		targets = append(targets, g-1, g, g+1)
	}
	compareSeeks(t, "spanning", data, targets)
}

// countingReadSeeker records how many bytes were read from it.
type countingReadSeeker struct {
	rs   *bytes.Reader
	read int64
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	n, err := c.rs.Read(p)
	c.read += int64(n)
	return n, err
}

func (c *countingReadSeeker) Seek(off int64, whence int) (int64, error) {
	return c.rs.Seek(off, whence)
}

// A seek must cost O(log n) page reads, not a pass over the file: a probe that reads a whole
// 128 KiB buffer, or a scan that restarts at byte 0, shows up here as megabytes.
func TestSeekToPageReadsLittle(t *testing.T) {
	// ~27 MB: 600 packets of 20-70 KB, so the cost of a full pass would be obvious.
	data := spanningStream(t, 600, 5)
	last := uint64(312 + 960*600)
	for _, frac := range []uint64{1, 2, 3} {
		target := last * frac / 4
		c := &countingReadSeeker{rs: bytes.NewReader(data)}
		r := NewPacketReader(c)
		r.SetMaxPacketSize(0)
		if _, err := r.SeekToPage(target); err != nil {
			t.Fatalf("target %d: %v", target, err)
		}
		if c.read > int64(len(data))/12 { // the search itself plus the read-ahead of the final page reader
			t.Errorf("target %d: seek read %d bytes of a %d byte file", target, c.read, len(data))
		}
	}

	// A small real file: the fixed costs dominate, but it still must not read all of it.
	if music, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		c := &countingReadSeeker{rs: bytes.NewReader(music)}
		if _, err := NewPacketReader(c).SeekToPage(2_000_000); err != nil {
			t.Fatal(err)
		}
		if c.read > int64(len(music))/2 {
			t.Errorf("seek read %d bytes of a %d byte file", c.read, len(music))
		}
	}
}

// A failed seek (target beyond the end) must leave the reader exactly where it was.
func TestFailedSeekLeavesReaderPositioned(t *testing.T) {
	data := spanningStream(t, 40, 3)
	ref := NewPacketReader(bytes.NewReader(data))
	ref.SetMaxPacketSize(0)
	r := NewPacketReader(bytes.NewReader(data))
	r.SetMaxPacketSize(0)

	for range 12 {
		a, err := ref.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		b, err := r.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Data, b.Data) {
			t.Fatal("readers diverged before the seek")
		}
	}
	if _, err := r.SeekToPage(math.MaxInt64); err == nil {
		t.Fatal("seeking far beyond the end must fail")
	}
	for i := range 10 {
		a, err := ref.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		b, err := r.ReadPacket()
		if err != nil {
			t.Fatalf("read %d after the failed seek: %v", i, err)
		}
		if !bytes.Equal(a.Data, b.Data) || a.GranulePosition != b.GranulePosition {
			t.Fatalf("read %d after the failed seek differs: the reader moved", i)
		}
	}
}

// Seeking to 0 after the reader has been through a chain used to fail with ErrSerialMismatch
// because the serial of the first stream was forgotten only on the way in.
func TestSeekToZeroAfterChain(t *testing.T) {
	a, _ := buildMockOpusStream(1, 2, 312, "a", [][]byte{{0xFC, 1}, {0xFC, 2}})
	b, _ := buildMockOpusStream(2, 2, 312, "b", [][]byte{{0xFC, 3}, {0xFC, 4}})
	r, err := NewOpusReader(bytes.NewReader(append(append([]byte(nil), a...), b...)))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := r.ReadAudioPacket(); err != nil {
			break
		}
	}
	if r.StreamIndex() != 1 || r.Tags.Comments[0] != "ARTIST=b" {
		t.Fatalf("expected to be in the second stream, got index %d tags %v", r.StreamIndex(), r.Tags.Comments)
	}
	if _, err := r.SeekToPage(0); err != nil {
		t.Fatalf("SeekToPage(0): %v", err)
	}
	if r.StreamIndex() != 0 || r.Tags.Comments[0] != "ARTIST=a" {
		t.Fatalf("after seeking to 0: stream index %d, tags %v; want the first stream", r.StreamIndex(), r.Tags.Comments)
	}
	pkt, err := r.ReadAudioPacket()
	if err != nil || pkt.Data[1] != 1 || pkt.StreamIndex != 0 || pkt.NewStream {
		t.Fatalf("first packet after the seek: %+v err %v", pkt, err)
	}
}
