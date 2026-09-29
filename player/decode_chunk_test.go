package player

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// Different read paths reach the decoder at different goroutine stack depths and with different
// partial-buffer state, but the decoded audio must not depend on how it is read: the PCM read in
// chunks of any size has to equal the PCM read in one go. The idea comes from kazzmir/opus-go
// (player/decode_chunk_regression_test.go); this version also covers multi-frame chunk sizes,
// float32 output, and a much longer stretch of audio. Chunk sizes are whole frames: Read returns 0
// bytes for a buffer smaller than one frame (see TestPlayer_ShortAndUnalignedReads), so io.ReadFull
// could never fill a partial frame.
func TestDecodeChunkSizeRegression(t *testing.T) {
	const length = 256 * 1024 // a multiple of every frame size used below (4 and 8 bytes)

	t.Run("int16", func(t *testing.T) {
		open := func() *OpusPlayer[int16] {
			p, err := NewPlayerFromFile(testFilePath, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close() })
			return p
		}
		checkChunked(t, open, length, []int{4, 12, 512, 1000, 8192, 65536})
	})
	t.Run("float32", func(t *testing.T) {
		open := func() *OpusPlayer[float32] {
			p, err := NewPlayerF32FromFile(testFilePath, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close() })
			return p
		}
		checkChunked(t, open, length, []int{8, 24, 512, 8192})
	})
}

func checkChunked[T interface{ int16 | float32 }](t *testing.T, open func() *OpusPlayer[T], length int, sizes []int) {
	t.Helper()
	var reference bytes.Buffer
	if _, err := io.CopyN(&reference, open(), int64(length)); err != nil {
		t.Fatal(err)
	}
	for _, size := range sizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			p := open()
			got := make([]byte, length)
			for offset := 0; offset < len(got); offset += size {
				if _, err := io.ReadFull(p, got[offset:min(offset+size, len(got))]); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(got, reference.Bytes()) {
				for i := range got {
					if got[i] != reference.Bytes()[i] {
						t.Fatalf("PCM differs at byte %d: got %02x want %02x", i, got[i], reference.Bytes()[i])
					}
				}
			}
		})
	}
}
