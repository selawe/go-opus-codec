package wav

import (
	"bytes"
	"testing"
)

func FuzzNewReader(f *testing.F) {
	f.Add([]byte("RIFF$\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x02\x00\x80\xbb\x00\x00\x00\xee\x02\x00\x04\x00\x10\x00data\x00\x00\x00\x00"))
	f.Add([]byte{})
	f.Add([]byte("RIFF"))

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewReader(bytes.NewReader(data))
		if err != nil {
			return
		}
		_ = r.SampleRate()
		_ = r.Channels()
		pcm := make([]int16, 512)
		const maxReads = 1000
		for range maxReads {
			if _, err := r.ReadInt16PCM(pcm); err != nil {
				return
			}
		}
	})
}
