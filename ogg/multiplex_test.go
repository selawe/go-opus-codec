package ogg

import (
	"bytes"
	"errors"
	"testing"
)

// foreignStream builds a stream that is not Opus: a BOS packet that is not OpusHead, a header packet and data
// packets whose granule positions count 44.1 kHz samples from a huge base, so any code that mixes them up with
// the Opus stream's granules produces visibly wrong results.
func foreignStream(t *testing.T, serial uint32, packets int, size ...int) []byte {
	t.Helper()
	pad := 0
	if len(size) > 0 {
		pad = size[0]
	}
	return foreignStreamGranules(t, serial, packets, pad, 5_000_000_000, 1024)
}

// foreignStreamGranules is foreignStream with the starting granule position and the step per packet chosen.
func foreignStreamGranules(t *testing.T, serial uint32, packets, size int, base, step uint64) []byte {
	t.Helper()
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, serial)
	if err := pw.WritePacket([]byte("\x01vorbis-identification"), 0, true, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.FlushPage(); err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket([]byte("\x03vorbis-comment"), 0, false, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.FlushPage(); err != nil {
		t.Fatal(err)
	}
	granule := base
	for i := 0; i < packets; i++ {
		granule += step
		pkt := append([]byte{0xAA, byte(i)}, make([]byte, size)...)
		if err := pw.WritePacket(pkt, granule, false, i == packets-1); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multiplex interleaves the pages of several streams the way a muxer does: every BOS page first, then the rest
// round-robin, so streams share the file and the last page may belong to any of them.
func multiplex(t *testing.T, streams ...[]byte) []byte {
	t.Helper()
	pages := make([][][]byte, len(streams))
	for i, s := range streams {
		pages[i] = splitPages(t, s)
	}
	var out bytes.Buffer
	for i := range pages {
		out.Write(pages[i][0]) // BOS pages, in the order given
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

func audioIDs(t *testing.T, r *OpusReader) []byte {
	t.Helper()
	var ids []byte
	for {
		p, err := r.ReadAudioPacket()
		if err != nil {
			return ids
		}
		ids = append(ids, p.Data[1])
	}
}

func opusPart(t *testing.T, serial uint32, name string, firstID byte, n int, size ...int) []byte {
	t.Helper()
	pk := make([][]byte, n)
	for i := range pk {
		pk[i] = []byte{0xFC, firstID + byte(i)}
		if len(size) > 0 {
			pk[i] = append(pk[i], make([]byte, size[0])...)
		}
	}
	b, err := buildMockOpusStream(serial, 2, 312, name, pk)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestOpusReader_SkipsForeignStreams(t *testing.T) {
	opus := opusPart(t, 10, "a", 1, 8)
	file := multiplex(t, foreignStream(t, 20, 40), opus) // the foreign BOS page comes first

	r, err := NewOpusReader(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("NewOpusReader on a multiplexed file: %v", err)
	}
	if r.Tags.Comments[0] != "ARTIST=a" || r.Head.Channels != 2 {
		t.Fatalf("headers: %+v %+v", r.Head, r.Tags)
	}
	want := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if got := audioIDs(t, r); !bytes.Equal(got, want) {
		t.Fatalf("audio packets %v, want %v", got, want)
	}
}

// The foreign stream's granule positions (a different scale, far larger, and filling the whole tail of the file)
// must not leak into the length or into seeking. The packets are large enough that the file is several hundred KB,
// so the seek really bisects and the last 64 KB hold only foreign pages.
func TestOpusReader_LengthAndSeekIgnoreForeignStreams(t *testing.T) {
	// The foreign granules may be far above the Opus ones (probes then look "past the target", which only makes a
	// seek start earlier) or far below them (they then look "before the target" although they sit at the end of the
	// file, which would start the final scan after the answer). Both must be harmless.
	for name, g := range map[string][2]uint64{"huge granules": {5_000_000_000, 1024}, "tiny granules": {0, 50}} {
		t.Run(name, func(t *testing.T) { lengthAndSeekIgnoreForeign(t, g[0], g[1]) })
	}
}

func lengthAndSeekIgnoreForeign(t *testing.T, base, step uint64) {
	const opusPackets, size = 120, 5000
	opus := opusPart(t, 10, "a", 1, opusPackets, size)
	alone, err := NewOpusReader(bytes.NewReader(opus))
	if err != nil {
		t.Fatal(err)
	}
	wantTotal, err := alone.TotalSamples()
	if err != nil {
		t.Fatal(err)
	}

	file := multiplex(t, opus, foreignStreamGranules(t, 20, 220, size, base, step)) // the foreign stream outlasts the Opus one
	if len(file) < 1<<20 {
		t.Fatalf("test file is only %d bytes", len(file))
	}
	r, err := NewOpusReader(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.TotalSamples(); err != nil || got != wantTotal {
		t.Fatalf("TotalSamples = %d, %v; want %d (the Opus stream alone)", got, err, wantTotal)
	}

	for _, target := range []uint64{1, 312 + 960*7 + 1, 312 + 960*60, 312 + 960*100 + 3, 312 + 960*119 + 5} {
		a, err := NewOpusReader(bytes.NewReader(opus))
		if err != nil {
			t.Fatal(err)
		}
		wantLast, err := a.SeekToPage(target)
		if err != nil {
			t.Fatalf("seek %d alone: %v", target, err)
		}
		wantIDs := audioIDs(t, a)

		m, err := NewOpusReader(bytes.NewReader(file))
		if err != nil {
			t.Fatal(err)
		}
		gotLast, err := m.SeekToPage(target)
		if err != nil {
			t.Fatalf("seek %d multiplexed: %v", target, err)
		}
		if gotLast != wantLast || !bytes.Equal(audioIDs(t, m), wantIDs) {
			t.Errorf("seek %d: multiplexed result differs from the stream alone (last %d vs %d)", target, gotLast, wantLast)
		}
	}

	// Seeking back to the start keeps the selection.
	if _, err := r.SeekToPage(312 + 960*10); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SeekToPage(0); err != nil {
		t.Fatal(err)
	}
	if got := audioIDs(t, r); len(got) != opusPackets || got[0] != 1 {
		t.Fatalf("after SeekToPage(0): %d packets, first %v", len(got), got)
	}
}

func TestOpusReader_ChoosesAmongSeveralOpusStreams(t *testing.T) {
	file := multiplex(t, opusPart(t, 10, "english", 1, 5), opusPart(t, 20, "bahasa", 101, 5), foreignStream(t, 30, 9))

	first, err := NewOpusReader(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if first.Tags.Comments[0] != "ARTIST=english" {
		t.Fatalf("default picked %v, want the first Opus stream", first.Tags.Comments)
	}

	second, err := NewOpusReaderWithOptions(bytes.NewReader(file), WithSerial(20))
	if err != nil {
		t.Fatal(err)
	}
	if second.Tags.Comments[0] != "ARTIST=bahasa" {
		t.Fatalf("WithSerial(20) picked %v", second.Tags.Comments)
	}
	if got := audioIDs(t, second); !bytes.Equal(got, []byte{101, 102, 103, 104, 105}) {
		t.Fatalf("audio of the chosen stream: %v", got)
	}

	// A selector can look at the stream itself, here at the tags page by way of the vendor-less head: pick the
	// stream whose OpusHead says 2 channels and skip the rest (all of them are stereo, so the first wins).
	sel, err := NewOpusReaderWithOptions(bytes.NewReader(file), WithStreamSelector(func(p []byte) bool {
		return bytes.HasPrefix(p, []byte("OpusHead")) && p[9] == 2
	}), WithVerifyCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	if sel.Tags.Comments[0] != "ARTIST=english" {
		t.Fatalf("selector picked %v", sel.Tags.Comments)
	}
}

func TestOpusReader_NoOpusStreamIsNotOpusOgg(t *testing.T) {
	file := multiplex(t, foreignStream(t, 20, 5), foreignStream(t, 30, 5))
	if _, err := NewOpusReader(bytes.NewReader(file)); !errors.Is(err, ErrNotOpusOgg) {
		t.Fatalf("err = %v, want ErrNotOpusOgg", err)
	}
	// ... and asking for a serial that is not in the file is the same.
	if _, err := NewOpusReaderWithOptions(bytes.NewReader(multiplex(t, opusPart(t, 10, "a", 1, 3))), WithSerial(99)); !errors.Is(err, ErrNotOpusOgg) {
		t.Fatalf("missing serial: err = %v, want ErrNotOpusOgg", err)
	}
}

// Without a selection the raw PacketReader behaves as before: a second serial is an error.
func TestPacketReader_WithoutSelectionStillRejectsOtherSerials(t *testing.T) {
	file := multiplex(t, opusPart(t, 10, "a", 1, 3), foreignStream(t, 20, 3))
	r := NewPacketReader(bytes.NewReader(file))
	var err error
	for i := 0; i < 50 && err == nil; i++ {
		_, err = r.ReadPacket()
	}
	if !errors.Is(err, ErrSerialMismatch) {
		t.Fatalf("err = %v, want ErrSerialMismatch", err)
	}
}

// A chain whose streams sit among foreign streams: each new stream is chosen by the same rule.
func TestOpusReader_ChainedStreamsAmongForeignOnes(t *testing.T) {
	a := multiplex(t, opusPart(t, 10, "a", 1, 4), foreignStream(t, 20, 6))
	b := multiplex(t, foreignStream(t, 21, 6), opusPart(t, 11, "b", 51, 4))
	r, err := NewOpusReader(bytes.NewReader(append(append([]byte(nil), a...), b...)))
	if err != nil {
		t.Fatal(err)
	}
	var ids []byte
	var newStreams int
	for {
		p, err := r.ReadAudioPacket()
		if err != nil {
			break
		}
		if p.NewStream {
			newStreams++
		}
		ids = append(ids, p.Data[1])
	}
	if !bytes.Equal(ids, []byte{1, 2, 3, 4, 51, 52, 53, 54}) || newStreams != 1 || r.Tags.Comments[0] != "ARTIST=b" {
		t.Fatalf("ids %v, new streams %d, tags %v", ids, newStreams, r.Tags.Comments)
	}
}
