package opus

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	libc "github.com/selawe/go-opus-codec/libcshim"
	"github.com/selawe/go-opus-codec/opusccenc"
)

// Repacketizer merges several Opus packets into a single packet, or splits
// a combined Opus packet into separate packets, without decoding and re-encoding.
//
// A single Opus packet can contain up to 48 frames, with a maximum cumulative duration of 120 ms.
// All methods on Repacketizer are thread-safe and protected by a mutex.
type Repacketizer struct {
	mu  sync.Mutex
	tls *libc.TLS
	st  uintptr
}

// NewRepacketizer creates and initializes a new Opus repacketizer.
func NewRepacketizer() (*Repacketizer, error) {
	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}

	st := opusccenc.Opus_opus_repacketizer_create(tls)
	if st == 0 {
		tls.Close()
		return nil, errors.New("opus: repacketizer_create failed")
	}

	rp := &Repacketizer{
		tls: tls,
		st:  st,
	}
	runtime.SetFinalizer(rp, (*Repacketizer).finalize)
	return rp, nil
}

func (rp *Repacketizer) finalize() {
	_ = rp.Close()
}

// Close frees all underlying C and TLS resources associated with the repacketizer.
func (rp *Repacketizer) Close() error {
	if rp == nil {
		return nil
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	runtime.SetFinalizer(rp, nil)
	if rp.tls != nil {
		if rp.st != 0 {
			opusccenc.Opus_opus_repacketizer_destroy(rp.tls, rp.st)
			rp.st = 0
		}
		rp.tls.Close()
		rp.tls = nil
	}
	return nil
}

// Reset resets the repacketizer state, clearing any previously concatenated frames.
func (rp *Repacketizer) Reset() error {
	if rp == nil {
		return errors.New("opus: repacketizer closed")
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.tls == nil || rp.st == 0 {
		return errors.New("opus: repacketizer closed")
	}
	opusccenc.Opus_opus_repacketizer_init(rp.tls, rp.st)
	return nil
}

// Cat adds an Opus packet to the repacketizer.
// Multiple packets can be added sequentially up to a maximum duration of 120 ms or 48 frames.
func (rp *Repacketizer) Cat(packet []byte) error {
	if rp == nil {
		return errors.New("opus: repacketizer closed")
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.tls == nil || rp.st == 0 {
		return errors.New("opus: repacketizer closed")
	}
	if len(packet) == 0 {
		return errors.New("opus: cannot repacketize empty packet")
	}

	dataPtr := libc.PtrByte(packet)
	ret := opusccenc.Opus_opus_repacketizer_cat(rp.tls, rp.st, dataPtr, int32(len(packet)))
	if ret != opusccenc.OPUS_OK {
		return fmt.Errorf("opus: repacketizer_cat failed: %s (%d)", opusccencErrorString(rp.tls, ret), ret)
	}
	runtime.KeepAlive(rp)
	return nil
}

// Frames returns the number of frames currently stored in the repacketizer.
func (rp *Repacketizer) Frames() int {
	if rp == nil {
		return 0
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.tls == nil || rp.st == 0 {
		return 0
	}
	n := int(opusccenc.Opus_opus_repacketizer_get_nb_frames(rp.tls, rp.st))
	runtime.KeepAlive(rp)
	return n
}

// Out outputs the combined repacketized packet into dst and returns the number of bytes written.
func (rp *Repacketizer) Out(dst []byte) (int, error) {
	if rp == nil {
		return 0, errors.New("opus: repacketizer closed")
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.tls == nil || rp.st == 0 {
		return 0, errors.New("opus: repacketizer closed")
	}
	if len(dst) == 0 {
		return 0, errors.New("opus: destination buffer is empty")
	}

	dstPtr := libc.PtrByte(dst)
	ret := opusccenc.Opus_opus_repacketizer_out(rp.tls, rp.st, dstPtr, int32(len(dst)))
	if ret < 0 {
		return 0, fmt.Errorf("opus: repacketizer_out failed: %s (%d)", opusccencErrorString(rp.tls, ret), ret)
	}
	runtime.KeepAlive(rp)
	return int(ret), nil
}

// OutRange outputs a selected range of frames [begin, end) from the repacketizer into dst.
func (rp *Repacketizer) OutRange(begin, end int, dst []byte) (int, error) {
	if rp == nil {
		return 0, errors.New("opus: repacketizer closed")
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.tls == nil || rp.st == 0 {
		return 0, errors.New("opus: repacketizer closed")
	}
	if begin < 0 || end <= begin {
		return 0, errors.New("opus: invalid frame range")
	}
	if len(dst) == 0 {
		return 0, errors.New("opus: destination buffer is empty")
	}

	dstPtr := libc.PtrByte(dst)
	ret := opusccenc.Opus_opus_repacketizer_out_range(rp.tls, rp.st, int32(begin), int32(end), dstPtr, int32(len(dst)))
	if ret < 0 {
		return 0, fmt.Errorf("opus: repacketizer_out_range failed: %s (%d)", opusccencErrorString(rp.tls, ret), ret)
	}
	runtime.KeepAlive(rp)
	return int(ret), nil
}
