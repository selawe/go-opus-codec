package ogg

import (
	"bytes"
	"io"
	"testing"
)

func BenchmarkCRC32(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}

	var header [27]byte
	copy(header[:4], "OggS")
	segTable := []byte{255, 255, 255, 255}

	var tbl [256]uint32
	const poly uint32 = 0x04C11DB7
	for i := 0; i < 256; i++ {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = (c << 1) ^ poly
			} else {
				c <<= 1
			}
		}
		tbl[i] = c
	}

	b.SetBytes(int64(len(header) + len(segTable) + len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = oggCRC3(header[:], segTable, data, tbl)
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
