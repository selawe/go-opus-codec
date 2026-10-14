package ogg

import (
	"bytes"
	"errors"
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

