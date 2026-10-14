package ogg

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestOpusTags_SetAddDelete(t *testing.T) {
	tags := OpusTags{
		Vendor:   "libopus 1.3",
		Comments: []string{"TITLE=Original Title", "ARTIST=Artist 1", "GENRE=Rock"},
	}

	if tags.Get("title") != "Original Title" {
		t.Errorf("expected 'Original Title', got '%s'", tags.Get("title"))
	}

	// Set existing
	tags.Set("Title", "New Title")
	if tags.Get("TITLE") != "New Title" {
		t.Errorf("expected 'New Title', got '%s'", tags.Get("TITLE"))
	}

	// Set new
	tags.Set("ALBUM", "My Album")
	if tags.Get("album") != "My Album" {
		t.Errorf("expected 'My Album', got '%s'", tags.Get("album"))
	}

	// Add multiple
	tags.Add("GENRE", "Pop")
	genres := tags.GetAll("genre")
	if len(genres) != 2 || genres[0] != "Rock" || genres[1] != "Pop" {
		t.Errorf("expected ['Rock', 'Pop'], got %v", genres)
	}

	// Delete
	tags.Delete("genre")
	if len(tags.GetAll("genre")) != 0 {
		t.Errorf("expected genre to be deleted")
	}
}

func TestRewriteOpusTags_Roundtrip(t *testing.T) {
	// 1. Build an Ogg Opus stream with initial tags and some audio packets
	head := testHead()
	origTags := OpusTags{
		Vendor:   "opusgo-test",
		Comments: []string{"TITLE=Initial Song", "ARTIST=Old Artist"},
	}

	var oggBuf bytes.Buffer
	pw := NewPacketWriter(&oggBuf, 0x12345678)
	if err := pw.WriteOpusHeaders(head, origTags); err != nil {
		t.Fatalf("WriteOpusHeaders: %v", err)
	}

	// Write 5 audio dummy packets
	for i := 0; i < 5; i++ {
		isEOS := i == 4
		audioPkt := []byte{0xf8, byte(i), 0x01, 0x02} // valid TOC format
		granule := uint64((i + 1) * 960)
		if err := pw.WritePacket(audioPkt, granule, false, isEOS); err != nil {
			t.Fatalf("WritePacket: %v", err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("pw.Close: %v", err)
	}

	// 2. Rewrite tags with new metadata
	newTags := OpusTags{
		Vendor:   "opusgo-updated",
		Comments: []string{"TITLE=Rewritten Song", "ARTIST=New Artist", "YEAR=2026"},
	}

	var rewrittenBuf bytes.Buffer
	if err := RewriteOpusTags(bytes.NewReader(oggBuf.Bytes()), &rewrittenBuf, newTags); err != nil {
		t.Fatalf("RewriteOpusTags: %v", err)
	}

	// 3. Verify rewritten stream by reading back via OpusReader
	r, err := NewOpusReader(bytes.NewReader(rewrittenBuf.Bytes()))
	if err != nil {
		t.Fatalf("NewOpusReader on rewritten stream: %v", err)
	}

	if r.Tags.Vendor != "opusgo-updated" {
		t.Errorf("expected vendor 'opusgo-updated', got '%s'", r.Tags.Vendor)
	}
	if r.Tags.Get("TITLE") != "Rewritten Song" {
		t.Errorf("expected TITLE 'Rewritten Song', got '%s'", r.Tags.Get("TITLE"))
	}
	if r.Tags.Get("YEAR") != "2026" {
		t.Errorf("expected YEAR '2026', got '%s'", r.Tags.Get("YEAR"))
	}

	// Verify all 5 audio packets survive intact
	count := 0
	for {
		pkt, err := r.ReadAudioPacket()
		if err != nil {
			break
		}
		if len(pkt.Data) != 4 || pkt.Data[1] != byte(count) {
			t.Errorf("packet %d corrupted: %v", count, pkt.Data)
		}
		count++
		if pkt.EOS {
			break
		}
	}
	if count != 5 {
		t.Fatalf("expected 5 audio packets, got %d", count)
	}
}

func TestRewriteOpusTags_LargeSpanningTags(t *testing.T) {
	// Test rewrite where new tags span multiple pages (> 65307 bytes)
	head := testHead()
	origTags := OpusTags{Vendor: "small", Comments: []string{"A=B"}}

	var oggBuf bytes.Buffer
	pw := NewPacketWriter(&oggBuf, 0xABCDEF01)
	if err := pw.WriteOpusHeaders(head, origTags); err != nil {
		t.Fatalf("WriteOpusHeaders: %v", err)
	}
	// 2 audio packets
	_ = pw.WritePacket([]byte{0xf8, 0x01}, 960, false, false)
	_ = pw.WritePacket([]byte{0xf8, 0x02}, 1920, false, true)
	_ = pw.Close()

	// New tags with 80 KB metadata picture
	bigArt := strings.Repeat("A", 80000)
	newBigTags := OpusTags{
		Vendor:   "big-tags",
		Comments: []string{"TITLE=Big", "METADATA_BLOCK_PICTURE=" + bigArt},
	}

	var rewrittenBuf bytes.Buffer
	if err := RewriteOpusTags(bytes.NewReader(oggBuf.Bytes()), &rewrittenBuf, newBigTags); err != nil {
		t.Fatalf("RewriteOpusTags with large tags: %v", err)
	}

	// Read with OpusReader to ensure valid CRC and framing across multiple pages
	r, err := NewOpusReader(bytes.NewReader(rewrittenBuf.Bytes()))
	if err != nil {
		t.Fatalf("NewOpusReader on large rewritten stream: %v", err)
	}

	if r.Tags.Get("TITLE") != "Big" {
		t.Errorf("expected TITLE 'Big', got '%s'", r.Tags.Get("TITLE"))
	}
	if len(r.Tags.Get("METADATA_BLOCK_PICTURE")) != 80000 {
		t.Errorf("picture tag size mismatch: got %d", len(r.Tags.Get("METADATA_BLOCK_PICTURE")))
	}

	// Check audio packets
	pkt1, err := r.ReadAudioPacket()
	if err != nil || len(pkt1.Data) != 2 || pkt1.Data[1] != 0x01 {
		t.Fatalf("pkt1 failed: %v, %v", err, pkt1)
	}
	pkt2, err := r.ReadAudioPacket()
	if err != nil || len(pkt2.Data) != 2 || pkt2.Data[1] != 0x02 || !pkt2.EOS {
		t.Fatalf("pkt2 failed: %v, %v", err, pkt2)
	}
}

func TestRewriteOpusTags_MissingTagsPageRejection(t *testing.T) {
	// Construct a stream that has OpusHead on page 0 and audio immediately on page 1 (missing OpusTags)
	head := testHead()
	var oggBuf bytes.Buffer
	pw := NewPacketWriter(&oggBuf, 0x11223344)

	// Write head
	headPkt, _ := BuildOpusHeadPacket(head)
	_ = pw.WritePacket(headPkt, 0, true, false)
	_ = pw.FlushPage()

	// Immediately write an audio packet on page 1 without OpusTags
	audioPkt := []byte{0xf8, 0x01, 0x02}
	_ = pw.WritePacket(audioPkt, 960, false, true)
	_ = pw.Close()

	// RewriteOpusTags must reject this stream with ErrHeaderSequence
	var out bytes.Buffer
	newTags := OpusTags{Vendor: "new", Comments: []string{"A=B"}}
	err := RewriteOpusTags(bytes.NewReader(oggBuf.Bytes()), &out, newTags)
	if !errors.Is(err, ErrHeaderSequence) {
		t.Fatalf("expected ErrHeaderSequence when OpusTags is missing, got: %v", err)
	}
}

func TestRewriteOpusTags_CorruptedAndTruncatedStreams(t *testing.T) {
	// Build a valid Ogg stream
	head := testHead()
	origTags := OpusTags{Vendor: "test", Comments: []string{"A=B"}}
	var oggBuf bytes.Buffer
	pw := NewPacketWriter(&oggBuf, 0x99887766)
	_ = pw.WriteOpusHeaders(head, origTags)
	for i := 0; i < 5; i++ {
		isEOS := i == 4
		_ = pw.WritePacket([]byte{0xf8, byte(i)}, uint64((i+1)*960), false, isEOS)
	}
	_ = pw.Close()

	validBytes := oggBuf.Bytes()

	// 1. Corrupted byte in middle of the stream (e.g. at offset 150)
	corrupted := make([]byte, len(validBytes))
	copy(corrupted, validBytes)
	corrupted[150] ^= 0xFF

	var outCorrupt bytes.Buffer
	newTags := OpusTags{Vendor: "new", Comments: []string{"A=B"}}
	err := RewriteOpusTags(bytes.NewReader(corrupted), &outCorrupt, newTags)
	if err == nil {
		t.Fatal("expected error on corrupted byte in stream, got nil")
	}

	// 2. Truncated tail (strip last 50 bytes)
	truncated := validBytes[:len(validBytes)-50]
	var outTrunc bytes.Buffer
	err = RewriteOpusTags(bytes.NewReader(truncated), &outTrunc, newTags)
	if err == nil {
		t.Fatal("expected error on truncated stream, got nil")
	}

	// 3. Stream missing EOS page (strip the final EOS page entirely)
	// The stream has 7 pages: page 0 (head), page 1 (tags), pages 2-6 (audio, 6 is EOS)
	pr := NewPageReader(bytes.NewReader(validBytes))
	pr.Resync = false
	var noEOSBuf bytes.Buffer
	for i := 0; i < 6; i++ { // omit the 7th page (EOS)
		p, err := pr.ReadPage()
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		_ = writeRawPage(&noEOSBuf, p)
	}

	var outNoEOS bytes.Buffer
	err = RewriteOpusTags(bytes.NewReader(noEOSBuf.Bytes()), &outNoEOS, newTags)
	if !errors.Is(err, ErrMissingEOS) {
		t.Fatalf("expected ErrMissingEOS on stream without EOS page, got: %v", err)
	}
}

// A truncated input must not look like a clean end: callers routinely treat errors.Is(err, io.EOF) as success.
func TestRewriteOpusTags_TruncatedInputIsNotEOF(t *testing.T) {
	music, err := os.ReadFile("../test/music_64kbps.opus")
	if err != nil {
		t.Skip(err)
	}
	headOnly := splitPages(t, music)[0] // OpusHead and then nothing: the OpusTags page is missing
	for name, in := range map[string][]byte{
		"empty input":         {},
		"only OpusHead":       headOnly,
		"cut at the end":      music[:len(music)-100],
		"cut inside the body": music[:len(music)/2+7],
	} {
		var out bytes.Buffer
		err := RewriteOpusTags(bytes.NewReader(in), &out, OpusTags{Vendor: "v"})
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if errors.Is(err, io.EOF) {
			t.Errorf("%s: err = %v satisfies errors.Is(err, io.EOF); a truncated file would be taken for a finished one", name, err)
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s: err = %v, want errors.Is(err, io.ErrUnexpectedEOF)", name, err)
		}
		if !errors.Is(err, ErrTruncatedPage) {
			t.Errorf("%s: err = %v must still match ErrTruncatedPage", name, err)
		}
	}
}

// RFC 3533: a page on which no packet ends carries granule position -1. OpusTags larger than one page spans several, and
// only the last of those pages (where the packet ends) may carry a real value (0 for a header). Strict demuxers care.
func TestRewriteOpusTags_SpanningTagPagesUseGranuleMinusOne(t *testing.T) {
	music, err := os.ReadFile("../test/music_64kbps.opus")
	if err != nil {
		t.Skip(err)
	}
	huge := OpusTags{Vendor: "v", Comments: []string{"COMMENT=" + strings.Repeat("x", 200_000)}} // ~4 pages
	var out bytes.Buffer
	if err := RewriteOpusTags(bytes.NewReader(music), &out, huge); err != nil {
		t.Fatal(err)
	}
	pages := readPages(t, out.Bytes())
	var tagPages []*Page
	for _, p := range pages[1:] {
		tagPages = append(tagPages, p)
		if len(p.SegmentTable) > 0 && p.SegmentTable[len(p.SegmentTable)-1] < 255 { // the packet ends here
			break
		}
	}
	if len(tagPages) < 3 {
		t.Fatalf("expected the tags to span several pages, got %d", len(tagPages))
	}
	for i, p := range tagPages {
		last := i == len(tagPages)-1
		switch {
		case !last && p.GranulePosition != ^uint64(0):
			t.Errorf("tag page %d (the packet continues): granule %#x, want -1", i, p.GranulePosition)
		case last && p.GranulePosition != 0:
			t.Errorf("last tag page: granule %#x, want 0", p.GranulePosition)
		}
		if wantContinued := i > 0; p.IsContinuedPacket() != wantContinued {
			t.Errorf("tag page %d: continued flag = %v, want %v", i, p.IsContinuedPacket(), wantContinued)
		}
	}
}

// RewriteOpusTags writes into someone's file, so it refuses tags that no reader should have to cope with.
func TestRewriteOpusTags_ValidatesNewTags(t *testing.T) {
	music, err := os.ReadFile("../test/music_64kbps.opus")
	if err != nil {
		t.Skip(err)
	}
	big := make([]string, 17)
	for i := range big {
		big[i] = "COMMENT=" + strings.Repeat("x", 1<<20)
	}
	bad := map[string]OpusTags{
		"no equals sign":               {Comments: []string{"JUSTTEXT"}},
		"empty field name":             {Comments: []string{"=value"}},
		"control character":            {Comments: []string{"KE\x01Y=v"}},
		"tilde in field name":          {Comments: []string{"KE~Y=v"}}, // 0x7E is outside 0x20-0x7D
		"non-ASCII field name":         {Comments: []string{"TÍTULO=v"}},
		"invalid UTF-8 value":          {Comments: []string{"TITLE=\xff\xfe"}},
		"invalid UTF-8 vendor":         {Vendor: "v\xff", Comments: []string{"A=b"}},
		"larger than a reader accepts": {Comments: big},
	}
	for name, tags := range bad {
		var out bytes.Buffer
		err := RewriteOpusTags(bytes.NewReader(music), &out, tags)
		if !errors.Is(err, ErrBadOpusTags) {
			t.Errorf("%s: err = %v, want ErrBadOpusTags", name, err)
		}
		if out.Len() != 0 {
			t.Errorf("%s: %d bytes were written although the tags are invalid", name, out.Len())
		}
	}

	good := OpusTags{Vendor: "vendör", Comments: []string{"TITLE=Ünïcode ✓", "EMPTY=", "lower=case", "A B=space in name", "MULTI=line1\nline2"}}
	var out bytes.Buffer
	if err := RewriteOpusTags(bytes.NewReader(music), &out, good); err != nil {
		t.Fatalf("valid tags rejected: %v", err)
	}
	r, err := NewOpusReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tags.Comments) != len(good.Comments) || r.Tags.Vendor != good.Vendor {
		t.Fatalf("tags did not round trip: %+v", r.Tags)
	}
}
