package test

import (
	"encoding/binary"
	"fmt"
	"io"
)

// BitstreamFrame represents a single frame/packet read from an RFC 6716 Section 6 .bit file.
type BitstreamFrame struct {
	PayloadLength  uint32
	WantFinalRange uint32
	Payload        []byte
	IsPacketLoss   bool
}

// BitstreamReader reads binary frames from an RFC 6716 bitstream file (.bit).
type BitstreamReader struct {
	r io.Reader
}

// NewBitstreamReader returns a new BitstreamReader reading from r.
func NewBitstreamReader(r io.Reader) *BitstreamReader {
	return &BitstreamReader{r: r}
}

// Next reads and returns the next frame from the bitstream.
// Returns io.EOF when the end of the bitstream is reached.
func (b *BitstreamReader) Next() (*BitstreamFrame, error) {
	var header [8]byte
	if _, err := io.ReadFull(b.r, header[:]); err != nil {
		return nil, err
	}

	payloadLen := binary.BigEndian.Uint32(header[0:4])
	wantFinalRange := binary.BigEndian.Uint32(header[4:8])

	// Max Opus packet per RFC 6716 is 1275 bytes * 48 frames = 61200 bytes.
	const maxPacketSize = 65536
	if payloadLen > maxPacketSize {
		return nil, fmt.Errorf("bitstream: payload length %d exceeds safety limit %d", payloadLen, maxPacketSize)
	}

	if payloadLen == 0 {
		return &BitstreamFrame{
			PayloadLength:  0,
			WantFinalRange: wantFinalRange,
			Payload:        nil,
			IsPacketLoss:   true,
		}, nil
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(b.r, payload); err != nil {
		return nil, fmt.Errorf("bitstream: failed to read payload: %w", err)
	}

	return &BitstreamFrame{
		PayloadLength:  payloadLen,
		WantFinalRange: wantFinalRange,
		Payload:        payload,
		IsPacketLoss:   false,
	}, nil
}
