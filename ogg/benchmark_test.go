package ogg

import (
	"bytes"
	"io"
	"testing"
)

var sink uint32

func BenchmarkCRC32(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	var header [27]byte
	copy(header[:4], "OggS")
	segTable := []byte{255, 255, 255, 255}

	b.SetBytes(int64(len(header) + len(segTable) + len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = oggCRC3(header[:], segTable, data)
	}
}

func BenchmarkPageReader(b *testing.B) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x1234)
	payload := make([]byte, 960)

	const numPages = 100
	for i := 0; i < numPages; i++ {
		_ = pw.WritePacket(payload, uint64(i*960), i == 0, i == numPages-1)
	}
	_ = pw.Flush()
	raw := buf.Bytes()

	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pr := NewPageReader(bytes.NewReader(raw))
		for {
			_, err := pr.ReadPage()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatalf("ReadPage: %v", err)
			}
		}
	}
}

// BenchmarkPageReader_Steady isolates page-parsing throughput from the
// bufio.Reader construction cost that BenchmarkPageReader pays on every
// b.N iteration (NewPageReader allocates a 128KB buffer). Here the
// PageReader and its underlying bufio buffer are created once and reset
// between iterations, mirroring the construction/steady-state split
// already used by BenchmarkPacketWriter vs BenchmarkPacketWriter_Streaming.
func BenchmarkPageReader_Steady(b *testing.B) {
	var buf bytes.Buffer
	pw := NewPacketWriter(&buf, 0x1234)
	payload := make([]byte, 960)

	const numPages = 100
	for i := 0; i < numPages; i++ {
		_ = pw.WritePacket(payload, uint64(i*960), i == 0, i == numPages-1)
	}
	_ = pw.Flush()
	raw := buf.Bytes()

	var br bytes.Reader
	br.Reset(raw)
	pr := NewPageReader(&br)

	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.Reset(raw)
		pr.r.Reset(&br)
		for {
			_, err := pr.ReadPage()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatalf("ReadPage: %v", err)
			}
		}
	}
}

func BenchmarkPacketWriter(b *testing.B) {
	packet := make([]byte, 960)
	b.SetBytes(int64(len(packet)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pw := NewPacketWriter(io.Discard, 0x1234)
		if err := pw.WritePacket(packet, uint64(i*960), i == 0, false); err != nil {
			b.Fatalf("WritePacket: %v", err)
		}
		_ = pw.Flush()
	}
}

func BenchmarkPacketWriter_Streaming(b *testing.B) {
	packet := make([]byte, 960)
	pw := NewPacketWriter(io.Discard, 0x1234)
	b.SetBytes(int64(len(packet)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := pw.WritePacket(packet, uint64(i*960), i == 0, false); err != nil {
			b.Fatalf("WritePacket: %v", err)
		}
	}
	_ = pw.Flush()
}
