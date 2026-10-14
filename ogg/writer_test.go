package ogg

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func readPages(t *testing.T, raw []byte) []*Page {
	t.Helper()
	pr := NewPageReader(bytes.NewReader(raw))
	var pages []*Page
	for {
		p, err := pr.ReadPage()
		if errors.Is(err, io.EOF) {
			return pages
		}
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, p)
	}
}

func testHead() OpusHead {
	return OpusHead{Version: 1, Channels: 2, PreSkip: 312, InputSampleRate: 48000}
}

// RFC 7845 section 3: OpusHead alone on the BOS page, OpusTags ending its page, audio on a fresh
// page, even when batching would happily pack them together.
func TestWriteOpusHeadersKeepsPageLayoutWhenBatching(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 5)
	pw.MaxPageSize = 100000 // batching on, with room for everything
	if err := pw.WriteOpusHeaders(testHead(), OpusTags{Vendor: "v", Comments: []string{"A=B"}}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if err := pw.WritePacket([]byte{0xFC, byte(i)}, 312+960*uint64(i+1), false, i == 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}

	pages := readPages(t, buf.Bytes())
	if len(pages) != 3 {
		t.Fatalf("%d pages, want head, tags and one batched audio page", len(pages))
	}
	if !pages[0].IsBOS() || len(pages[0].SegmentTable) != 1 || pages[0].IsEOS() {
		t.Errorf("page 0 must be OpusHead alone with BOS: %+v", pages[0])
	}
	if pages[1].IsBOS() || string(pages[1].SegmentData[:8]) != "OpusTags" || pages[1].IsEOS() {
		t.Errorf("page 1 must be the OpusTags page")
	}
	if len(pages[2].SegmentTable) != 3 || !pages[2].IsEOS() {
		t.Errorf("page 2 must hold the three audio packets and end the stream: %d segments, eos=%v", len(pages[2].SegmentTable), pages[2].IsEOS())
	}
}

// Forgetting eos=true on the last packet leaves the stream unterminated; Close flags the page
// that is still buffered instead.
func TestCloseFlagsThePendingPageAsEOS(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 5)
	pw.MaxPageSize = 100000
	if err := pw.WriteOpusHeaders(testHead(), OpusTags{Vendor: "v"}); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if err := pw.WritePacket([]byte{0xFC, byte(i)}, 312+960*uint64(i+1), false, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	pages := readPages(t, buf.Bytes())
	if last := pages[len(pages)-1]; !last.IsEOS() {
		t.Fatal("the final page is not flagged EOS")
	}
	if err := pw.Close(); err != nil { // idempotent
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseWithoutAnyEOSReportsIt(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 5) // 1:1 mode: every packet is written at once, nothing stays pending
	if err := pw.WriteOpusHeaders(testHead(), OpusTags{Vendor: "v"}); err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket([]byte{0xFC, 1}, 312+960, false, false); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); !errors.Is(err, ErrMissingEOS) {
		t.Fatalf("Close = %v, want ErrMissingEOS", err)
	}
	if err := pw.Close(); !errors.Is(err, ErrMissingEOS) {
		t.Fatalf("a repeated Close must repeat the error, got %v", err)
	}
}

func TestWritesAfterEOSOrCloseAreRefused(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 5)
	if err := pw.WriteOpusHeaders(testHead(), OpusTags{Vendor: "v"}); err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket([]byte{0xFC, 1}, 312+960, false, true); err != nil {
		t.Fatal(err)
	}
	if err := pw.WritePacket([]byte{0xFC, 2}, 312+1920, false, false); !errors.Is(err, ErrWriteAfterEOS) {
		t.Fatalf("write after EOS: %v, want ErrWriteAfterEOS", err)
	}
	if err := pw.Close(); err != nil { // Close flushes the buffered pages, so measure afterwards
		t.Fatal(err)
	}
	n := buf.Len()
	if err := pw.WritePacket([]byte{0xFC, 3}, 0, false, false); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("write after Close: %v, want ErrWriterClosed", err)
	}
	if err := pw.Flush(); err != nil || buf.Len() != n {
		t.Fatalf("bytes were written after the stream ended (flush err %v, %d -> %d)", err, n, buf.Len())
	}
}

type failAfter struct {
	n   int
	err error
	w   bytes.Buffer
}

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	if len(p) > f.n {
		p = p[:f.n]
		f.n = 0
		f.w.Write(p)
		return len(p), f.err
	}
	f.n -= len(p)
	return f.w.Write(p)
}

// After a failed write the page buffers hold a partial page; retrying would emit corrupt or
// duplicated data, so the first error must stick.
func TestWriteErrorIsSticky(t *testing.T) {
	boom := errors.New("disk full")
	sink := &failAfter{n: 100, err: boom}
	pw := NewPacketWriter(sink, 5)
	pw.MaxPageSize = 0

	var first error
	for i := 0; i < 50 && first == nil; i++ {
		first = pw.WritePacket(bytes.Repeat([]byte{byte(i)}, 300), 960*uint64(i+1), i == 0, false)
		if first == nil {
			first = pw.Flush() // the 1 MiB bufio only hits the sink on a flush
		}
	}
	if !errors.Is(first, boom) {
		t.Fatalf("first error = %v, want the sink's", first)
	}
	written := sink.w.Len()
	for _, call := range []func() error{
		func() error { return pw.WritePacket([]byte{1}, 1, false, false) },
		pw.Flush,
		pw.FlushPage,
		pw.Close,
	} {
		if err := call(); !errors.Is(err, boom) {
			t.Fatalf("later call returned %v, want the sticky error", err)
		}
	}
	if sink.w.Len() != written {
		t.Fatal("more bytes reached the sink after the failure")
	}
}
