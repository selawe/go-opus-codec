package opus

import (
	"errors"
	"fmt"
	"math"
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

// maxPadInput bounds the packet handed to the pad helpers: opus_packet_pad works on a
// copy of the input in the codec's fixed-size scratch stack, and a larger packet aborts
// the process-wide "pseudostack overflow" path. No unpadded Opus packet comes near this
// (the RFC 6716 maximum is 61,440 bytes).
const maxPadInput = 100000

// ErrPacketTooLarge is returned by the pad helpers for packets, or target lengths, beyond
// what they support.
var ErrPacketTooLarge = errors.New("opus: packet too large")

// withPadTLS runs fn with a pooled TLS. A panic raised by the transpiled code (for
// example an internal assertion) is returned as an error instead of unwinding the
// caller, and that TLS is dropped rather than pooled because its pseudo-stack state
// can no longer be trusted.
func withPadTLS(fn func(tls *libc.TLS)) (err error) {
	tls, err := getPadTLS()
	if err != nil {
		return err
	}
	clean := false
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("opus: internal codec error: %v", r)
			return
		}
		if clean {
			putPadTLS(tls)
		}
	}()
	fn(tls)
	clean = true
	return nil
}

func checkPadSizes(packetLen, newLen int) error {
	if packetLen > maxPadInput {
		return fmt.Errorf("%w: %d bytes (limit %d)", ErrPacketTooLarge, packetLen, maxPadInput)
	}
	if newLen > math.MaxInt32 {
		return fmt.Errorf("%w: target length %d", ErrPacketTooLarge, newLen)
	}
	return nil
}

// PacketPad pads an Opus packet to newLen bytes.
// A new buffer of size newLen is allocated and returned with padding applied.
func PacketPad(packet []byte, newLen int) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot pad empty packet")
	}
	if newLen < len(packet) {
		return nil, fmt.Errorf("opus: newLen %d is smaller than packet length %d", newLen, len(packet))
	}
	if err := checkPadSizes(len(packet), newLen); err != nil {
		return nil, err
	}
	if newLen == len(packet) {
		dup := make([]byte, len(packet))
		copy(dup, packet)
		return dup, nil
	}

	buf := make([]byte, newLen)
	copy(buf, packet)

	var ret int32
	var msg string
	if err := withPadTLS(func(tls *libc.TLS) {
		ret = opusccenc.Opus_opus_packet_pad(tls, libc.PtrByte(buf), int32(len(packet)), int32(newLen))
		if ret != opusccenc.OPUS_OK {
			msg = opusccencErrorString(tls, ret)
		}
	}); err != nil {
		return nil, err
	}
	if ret != opusccenc.OPUS_OK {
		return nil, fmt.Errorf("opus: packet_pad failed: %s (%d)", msg, ret)
	}
	return buf, nil
}

// PacketUnpad removes padding from an Opus packet and returns the unpadded slice.
func PacketUnpad(packet []byte) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: cannot unpad empty packet")
	}
	if err := checkPadSizes(len(packet), 0); err != nil {
		return nil, err
	}

	buf := make([]byte, len(packet))
	copy(buf, packet)

	var ret int32
	var msg string
	if err := withPadTLS(func(tls *libc.TLS) {
		ret = opusccenc.Opus_opus_packet_unpad(tls, libc.PtrByte(buf), int32(len(buf)))
		if ret < 0 {
			msg = opusccencErrorString(tls, ret)
		}
	}); err != nil {
		return nil, err
	}
	if ret < 0 {
		return nil, fmt.Errorf("opus: packet_unpad failed: %s (%d)", msg, ret)
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
	if err := checkPadSizes(len(packet), newLen); err != nil {
		return nil, err
	}
	if newLen == len(packet) {
		dup := make([]byte, len(packet))
		copy(dup, packet)
		return dup, nil
	}

	buf := make([]byte, newLen)
	copy(buf, packet)

	var ret int32
	var msg string
	if err := withPadTLS(func(tls *libc.TLS) {
		ret = opusccenc.Opus_opus_multistream_packet_pad(tls, libc.PtrByte(buf), int32(len(packet)), int32(newLen), int32(nbStreams))
		if ret != opusccenc.OPUS_OK {
			msg = opusccencErrorString(tls, ret)
		}
	}); err != nil {
		return nil, err
	}
	if ret != opusccenc.OPUS_OK {
		return nil, fmt.Errorf("opus: multistream_packet_pad failed: %s (%d)", msg, ret)
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
	if err := checkPadSizes(len(packet), 0); err != nil {
		return nil, err
	}

	buf := make([]byte, len(packet))
	copy(buf, packet)

	var ret int32
	var msg string
	if err := withPadTLS(func(tls *libc.TLS) {
		ret = opusccenc.Opus_opus_multistream_packet_unpad(tls, libc.PtrByte(buf), int32(len(buf)), int32(nbStreams))
		if ret < 0 {
			msg = opusccencErrorString(tls, ret)
		}
	}); err != nil {
		return nil, err
	}
	if ret < 0 {
		return nil, fmt.Errorf("opus: multistream_packet_unpad failed: %s (%d)", msg, ret)
	}
	out := make([]byte, ret)
	copy(out, buf[:ret])
	return out, nil
}
