// Package opusgo provides a high-level, pure-Go API for encoding, decoding,
// and playing Ogg Opus audio streams without cgo or external C libraries.
//
// It provides:
//   - High-level conversion helpers (EncodeWAVToOggOpus, DecodeOggOpusToWAV,
//     ConvertWAVFileToOggOpus, and ConvertOggOpusFileToWAV).
//   - OpusPlayer for straightforward playback, frame-accurate seeking, and streaming
//     of Opus audio files and streams.
//
// For lower-level access to Opus decoding, encoding, and packet inspection,
// see the sub-packages:
//   - opus: Low-level Opus encoder, decoder, and RFC 6716 / RFC 8251 packet utilities.
//   - ogg: RFC 3533 Ogg bitstream and RFC 7845 Ogg Opus container demuxer/muxer.
//   - player: High-level streaming player with int16 and float32 PCM output.
//   - wav: Reading and writing of 16-bit linear PCM WAV files.
package opusgo

import (
	"io"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/player"
)

// OpusPlayer is the high-level Ogg Opus audio player supporting int16 or float32 PCM.
type OpusPlayer[T player.DataType] = player.OpusPlayer[T]

// OpusSampleRateHz is the standard decoded sample rate for Opus audio streams (48000 Hz).
const OpusSampleRateHz = ogg.OpusSampleRateHz

// NewPlayerFromReader creates a new OpusPlayer from an io.Reader that produces int16 PCM samples.
// If the reader implements io.Seeker, frame-accurate seeking within the stream will be supported.
func NewPlayerFromReader(reader io.Reader) (*OpusPlayer[int16], error) {
	return player.NewPlayerFromReader(reader)
}

// NewPlayerF32FromReader creates a new OpusPlayer from an io.Reader that produces normalized float32 PCM samples in [-1.0, 1.0].
// If the reader implements io.Seeker, frame-accurate seeking within the stream will be supported.
func NewPlayerF32FromReader(reader io.Reader) (*OpusPlayer[float32], error) {
	return player.NewPlayerF32FromReader(reader)
}

// NewPlayerFromFile creates a new OpusPlayer from a file path producing int16 PCM samples.
// If stream is true, the file will be streamed on-demand instead of fully loaded into memory.
// Calling player.Close() releases the underlying file handle and decoder resources.
func NewPlayerFromFile(path string, stream bool) (*OpusPlayer[int16], error) {
	return player.NewPlayerFromFile(path, stream)
}

// NewPlayerF32FromFile creates a new OpusPlayer from a file path producing normalized float32 PCM samples in [-1.0, 1.0].
// If stream is true, the file will be streamed on-demand instead of fully loaded into memory.
// Calling player.Close() releases the underlying file handle and decoder resources.
func NewPlayerF32FromFile(path string, stream bool) (*OpusPlayer[float32], error) {
	return player.NewPlayerF32FromFile(path, stream)
}
