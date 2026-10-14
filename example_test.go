package opusgo_test

import (
	"bytes"
	"fmt"
	"io"
	"log"

	opusgo "github.com/selawe/go-opus-codec"
	"github.com/selawe/go-opus-codec/wav"
)

// seekBuffer is an in-memory io.WriteSeeker, which wav.Writer needs to patch the sizes into the
// header when it is closed. Real programs would pass an *os.File.
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

// oneSecondWAV returns one second of a 440 Hz tone as a 48 kHz stereo 16-bit WAV file.
func oneSecondWAV() []byte {
	out := &seekBuffer{}
	w, err := wav.NewWriter(out, 48000, 2)
	if err != nil {
		log.Fatal(err)
	}
	pcm := make([]int16, 0, 2*48000)
	for i := 0; i < 48000; i++ {
		v := int16(8000 * float64(i%109-54) / 54) // a crude 440 Hz saw, enough to have something to encode
		pcm = append(pcm, v, v)
	}
	if err := w.WriteInt16PCM(pcm); err != nil {
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	return out.b
}

// Encode a WAV file to Ogg Opus. Any sample rate works: rates libopus does not support, such as
// 44.1 kHz, are resampled to 48 kHz first.
func ExampleEncodeWAVToOggOpus() {
	var ogg bytes.Buffer
	err := opusgo.EncodeWAVToOggOpus(bytes.NewReader(oneSecondWAV()), &ogg, &opusgo.EncodeOptions{
		Bitrate:     64000,
		Application: opusgo.ApplicationAudio,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Ogg stream:", string(ogg.Bytes()[:4]))
	fmt.Println("smaller than the PCM:", ogg.Len() < 48000*4)
	// Output:
	// Ogg stream: OggS
	// smaller than the PCM: true
}

// Decode Ogg Opus to WAV. Always set an output limit for input you do not trust: a few bytes of Opus
// can decode to 120 ms of audio.
func ExampleDecodeOggOpusToWAV() {
	var ogg bytes.Buffer
	if err := opusgo.EncodeWAVToOggOpus(bytes.NewReader(oneSecondWAV()), &ogg, nil); err != nil {
		log.Fatal(err)
	}

	out := &seekBuffer{}
	err := opusgo.DecodeOggOpusToWAV(bytes.NewReader(ogg.Bytes()), out,
		opusgo.WithMaxOutputBytes(opusgo.DefaultMaxOutputBytes))
	if err != nil {
		log.Fatal(err)
	}

	r, err := wav.NewReader(bytes.NewReader(out.b))
	if err != nil {
		log.Fatal(err)
	}
	total := 0
	pcm := make([]int16, 4800)
	for {
		n, err := r.ReadInt16PCM(pcm)
		total += n
		if err != nil {
			break
		}
	}
	fmt.Printf("%d Hz, %d channels, %d samples per channel\n", r.SampleRate(), r.Channels(), total/r.Channels())
	// Output:
	// 48000 Hz, 2 channels, 48000 samples per channel
}
