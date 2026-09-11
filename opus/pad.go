package opus

import (
	"errors"
	"fmt"
	"sync"

	libc "github.com/selawe/go-opus-codec/libcshim"
	"github.com/selawe/go-opus-codec/opusccenc"
)

// padTLSPool holds *libc.TLS instances for the stateless pad/unpad helpers
// below. Unlike Encoder/Decoder (which own one TLS for their whole
// lifetime), these are one-shot functions - without pooling, each call paid
// for a fresh NewTLS (64KB stack chunk + heap/key maps), only to tear it
// all down again on return.
//
// Critically, putPadTLS does NOT call FreePseudostackTLS: profiling showed the
// dominant cost here isn't that outer TLS shell, it's opus_packet_pad's own
// GLOBAL_STACK_SIZE scratch buffer, which the transpiled code lazily
// mallocs once and caches on the TLS under a pthread-key (see
// opusccenc.Opus_opus_packet_pad_impl) for reuse across calls - exactly
// like Encoder/Decoder already do for their entire lifetime, only ever
// calling FreePseudostackTLS in Close(). Calling FreePseudostackTLS on every
// Put would tear down that cache and force it to be re-mallocated on the
// very next borrow, defeating the pool. Letting a *TLS simply fall out of
// the pool (and get GC'd) is fine: everything it holds is plain Go memory,
// not a real C allocation requiring an explicit free.
var padTLSPool = sync.Pool{
	New: func() any { return libc.NewTLS() },
}

func getPadTLS() (*libc.TLS, error) {
	tls, _ := padTLSPool.Get().(*libc.TLS)
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}
	return tls, nil
}

func putPadTLS(tls *libc.TLS) {
	if tls == nil {
		return
	}
	tls.Reset()
	padTLSPool.Put(tls)
}

// PacketPad pads an Opus packet to newLen bytes.
// If cap(packet) >= newLen, the padding is written in-place and packet[:newLen] is returned.
// Otherwise, a new buffer of size newLen is allocated and returned with padding applied.
func PacketPad(packet []byte, newLen int) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot pad empty packet")
	}
	if newLen < len(packet) {
		return nil, fmt.Errorf("opus: newLen %d is smaller than packet length %d", newLen, len(packet))
	}
	if newLen == len(packet) {
		dup := make([]byte, len(packet))
		copy(dup, packet)
		return dup, nil
	}

	buf := make([]byte, newLen)
	copy(buf, packet)

	tls, err := getPadTLS()
	if err != nil {
		return nil, err
	}
	defer putPadTLS(tls)

	ret := opusccenc.Opus_opus_packet_pad(tls, libc.PtrByte(buf), int32(len(packet)), int32(newLen))
	if ret != opusccenc.OPUS_OK {
		return nil, fmt.Errorf("opus: packet_pad failed: %s (%d)", opusccencErrorString(tls, ret), ret)
	}
	return buf, nil
}

// PacketUnpad removes padding from an Opus packet and returns the unpadded slice.
func PacketUnpad(packet []byte) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot unpad empty packet")
	}

	buf := make([]byte, len(packet))
	copy(buf, packet)

	tls, err := getPadTLS()
	if err != nil {
		return nil, err
	}
	defer putPadTLS(tls)

	ret := opusccenc.Opus_opus_packet_unpad(tls, libc.PtrByte(buf), int32(len(buf)))
	if ret < 0 {
		return nil, fmt.Errorf("opus: packet_unpad failed: %s (%d)", opusccencErrorString(tls, ret), ret)
	}
	out := make([]byte, ret)
	copy(out, buf[:ret])
	return out, nil
}

// MultistreamPacketPad pads a multistream Opus packet to newLen bytes.
func MultistreamPacketPad(packet []byte, newLen int, nbStreams int) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot pad empty packet")
	}
	if nbStreams <= 0 {
		return nil, errors.New("opus: invalid nbStreams")
	}
	if newLen < len(packet) {
		return nil, fmt.Errorf("opus: newLen %d is smaller than packet length %d", newLen, len(packet))
	}
	if newLen == len(packet) {
		dup := make([]byte, len(packet))
		copy(dup, packet)
		return dup, nil
	}

	buf := make([]byte, newLen)
	copy(buf, packet)

	tls, err := getPadTLS()
	if err != nil {
		return nil, err
	}
	defer putPadTLS(tls)

	ret := opusccenc.Opus_opus_multistream_packet_pad(tls, libc.PtrByte(buf), int32(len(packet)), int32(newLen), int32(nbStreams))
	if ret != opusccenc.OPUS_OK {
		return nil, fmt.Errorf("opus: multistream_packet_pad failed: %s (%d)", opusccencErrorString(tls, ret), ret)
	}
	return buf, nil
}

// MultistreamPacketUnpad removes padding from a multistream Opus packet and returns the unpadded slice.
func MultistreamPacketUnpad(packet []byte, nbStreams int) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot unpad empty packet")
	}
	if nbStreams <= 0 {
		return nil, errors.New("opus: invalid nbStreams")
	}

	buf := make([]byte, len(packet))
	copy(buf, packet)

	tls, err := getPadTLS()
	if err != nil {
		return nil, err
	}
	defer putPadTLS(tls)

	ret := opusccenc.Opus_opus_multistream_packet_unpad(tls, libc.PtrByte(buf), int32(len(buf)), int32(nbStreams))
	if ret < 0 {
		return nil, fmt.Errorf("opus: multistream_packet_unpad failed: %s (%d)", opusccencErrorString(tls, ret), ret)
	}
	out := make([]byte, ret)
	copy(out, buf[:ret])
	return out, nil
}
