package wav_test

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/selawe/go-opus-codec/wav"
)

type seekBuffer struct {
	b   []byte
	pos int
}

func (s *seekBuffer) Write(p []byte) (int, error) {
	if end := s.pos + len(p); end > len(s.b) {
		s.b = append(s.b, make([]byte, end-len(s.b))...)
	}
	copy(s.b[s.pos:], p)
	s.pos += len(p)
	return len(p), nil
}

func (s *seekBuffer) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.pos = int(off)
	case io.SeekCurrent:
		s.pos += int(off)
	case io.SeekEnd:
		s.pos = len(s.b) + int(off)
	}
	return int64(s.pos), nil
}

// Write a stereo WAV file, then read it back as int16 and as float32. The Reader also understands
// 8/24/32-bit integer and 32-bit float files, extensible headers and RF64.
func Example() {
	out := &seekBuffer{}
	w, err := wav.NewWriter(out, 44100, 2)
	if err != nil {
		log.Fatal(err)
	}
	if err := w.WriteInt16PCM([]int16{1000, -1000, 2000, -2000}); err != nil { // two stereo frames
		log.Fatal(err)
	}
	if err := w.Close(); err != nil { // patches the sizes into the header
		log.Fatal(err)
	}

	r, err := wav.NewReader(bytes.NewReader(out.b))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(r.SampleRate(), "Hz,", r.Channels(), "channels,", r.BitsPerSample(), "bit")

	f := make([]float32, 4)
	n, _ := r.ReadFloat32PCM(f)
	fmt.Printf("%d samples, first %.4f\n", n, f[0])
	// Output:
	// 44100 Hz, 2 channels, 16 bit
	// 4 samples, first 0.0305
}
