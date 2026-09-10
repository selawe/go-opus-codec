package ogg

import (
	"bytes"
	"os"
	"testing"
)

// FuzzOpusReader feeds arbitrary bytes into the Ogg Opus container demuxer
// (RFC 3533 page parsing + RFC 7845 header parsing) and drains it as an
// io.Reader-based consumer would. It only checks that no input can crash the
// reader or send it into an unbounded loop; errors from malformed input are
// expected and not themselves a failure.
func FuzzOpusReader(f *testing.F) {
	f.Add([]byte("OggS"))
	if data, err := os.ReadFile("../test/music_64kbps.opus"); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewOpusReader(bytes.NewReader(data))
		if err != nil {
			return
		}

		const maxPackets = 100000
		for range maxPackets {
			if _, err := r.ReadAudioPacket(); err != nil {
				return
			}
		}
		t.Fatalf("ReadAudioPacket did not terminate within %d packets", maxPackets)
	})
}
