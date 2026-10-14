// Package opusgo provides a high-level, pure-Go API for encoding, decoding,
// and playing Ogg Opus audio streams without cgo or external C libraries.
//
// It provides:
//   - High-level conversion helpers (EncodeWAVToOggOpus, DecodeOggOpusToWAV,
//     ConvertWAVFileToOggOpus, and ConvertOggOpusFileToWAV) with Context cancellation support.
//   - OpusPlayer for straightforward playback, frame-accurate seeking, and streaming
//     of Opus audio files and streams.
//   - Fast in-place OpusTags metadata editing without re-encoding audio (RewriteOpusTags).
//
// For lower-level access to Opus decoding, encoding, packet inspection, and networking,
// see the sub-packages:
//   - opus: Low-level Opus encoder, decoder, RFC 6716 / RFC 8251 packet utilities, and ITU-R BS.775 downmixers.
//   - ogg: RFC 3533 Ogg bitstream and RFC 7845 Ogg Opus container demuxer/muxer.
//   - rtp: RFC 7587 / RFC 3550 Opus RTP packetizer and depacketizer for WebRTC and VoIP.
//   - player: High-level streaming player with int16 and float32 PCM output.
//   - wav: Reading and writing of 16-bit linear PCM WAV files.
package opusgo

import (
	"io"

	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/player"
)

// RewriteOpusTags streams an Ogg Opus file from r to w, replacing its metadata tags
// without re-encoding any audio packets.
func RewriteOpusTags(r io.Reader, w io.Writer, newTags ogg.OpusTags) error {
	return ogg.RewriteOpusTags(r, w, newTags)
}

// OpusPlayer is the high-level Ogg Opus audio player supporting int16 or float32 PCM.
type OpusPlayer[T player.DataType] = player.OpusPlayer[T]

// OpusSampleRateHz is the standard decoded sample rate for Opus audio streams (48000 Hz).
const OpusSampleRateHz = ogg.OpusSampleRateHz

// NewPlayerFromReader creates a new OpusPlayer from an io.Reader that produces int16 PCM samples.
// If the reader implements io.Seeker, frame-accurate seeking within the stream will be supported.
func NewPlayerFromReader(reader io.Reader, opts ...ogg.ReaderOption) (*OpusPlayer[int16], error) {
	return player.NewPlayerFromReader(reader, opts...)
}

// NewPlayerF32FromReader creates a new OpusPlayer from an io.Reader that produces normalized float32 PCM samples in [-1.0, 1.0].
// If the reader implements io.Seeker, frame-accurate seeking within the stream will be supported.
func NewPlayerF32FromReader(reader io.Reader, opts ...ogg.ReaderOption) (*OpusPlayer[float32], error) {
	return player.NewPlayerF32FromReader(reader, opts...)
}

// NewPlayerFromFile creates a new OpusPlayer from a file path producing int16 PCM samples.
// If stream is true, the file will be streamed on-demand instead of fully loaded into memory.
// Calling player.Close() releases the underlying file handle and decoder resources.
func NewPlayerFromFile(path string, stream bool, opts ...ogg.ReaderOption) (*OpusPlayer[int16], error) {
	return player.NewPlayerFromFile(path, stream, opts...)
}

// NewPlayerF32FromFile creates a new OpusPlayer from a file path producing normalized float32 PCM samples in [-1.0, 1.0].
// If stream is true, the file will be streamed on-demand instead of fully loaded into memory.
// Calling player.Close() releases the underlying file handle and decoder resources.
func NewPlayerF32FromFile(path string, stream bool, opts ...ogg.ReaderOption) (*OpusPlayer[float32], error) {
	return player.NewPlayerF32FromFile(path, stream, opts...)
}
