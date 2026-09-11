package opus

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	libc "github.com/selawe/go-opus-codec/libcshim"
	"github.com/selawe/go-opus-codec/opuscc"
)

// SoftClipper applies non-linear soft clipping to float32 PCM samples, smoothly compressing
// peaks exceeding [-1.0, 1.0] while preserving continuity across buffer boundaries.
type SoftClipper struct {
	mu       sync.Mutex
	channels int
	mem      []float32
	tls      *libc.TLS
}

// NewSoftClipper creates a stateful SoftClipper for the given channel count.
func NewSoftClipper(channels int) (*SoftClipper, error) {
	if channels <= 0 {
		return nil, errors.New("opus: invalid channel count")
	}
	tls := libc.NewTLS()
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}
	sc := &SoftClipper{
		channels: channels,
		mem:      make([]float32, channels),
		tls:      tls,
	}
	runtime.SetFinalizer(sc, (*SoftClipper).finalize)
	return sc, nil
}

func (sc *SoftClipper) finalize() {
	_ = sc.Close()
}

// Process applies soft clipping in-place to interleaved pcm samples.
func (sc *SoftClipper) Process(pcm []float32) error {
	if sc == nil {
		return errors.New("opus: soft clipper closed")
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.tls == nil {
		return errors.New("opus: soft clipper closed")
	}
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm)%sc.channels != 0 {
		return fmt.Errorf("opus: pcm sample count %d must be multiple of channels %d", len(pcm), sc.channels)
	}
	nbSamples := int32(len(pcm) / sc.channels)
	dataPtr := libc.PtrFloat32(pcm)
	memPtr := libc.PtrFloat32(sc.mem)

	opuscc.Opus_opus_pcm_soft_clip(sc.tls, dataPtr, nbSamples, int32(sc.channels), memPtr)
	runtime.KeepAlive(sc)
	return nil
}

// Reset clears the clipping history memory.
func (sc *SoftClipper) Reset() {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	clear(sc.mem)
}

// Close releases the underlying TLS resources.
func (sc *SoftClipper) Close() error {
	if sc == nil {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	runtime.SetFinalizer(sc, nil)
	if sc.tls != nil {
		opuscc.FreePseudostackTLS(sc.tls)
		sc.tls.Close()
		sc.tls = nil
	}
	return nil
}

// softClipTLSPool holds *libc.TLS instances for the stateless SoftClip
// helper below. It mirrors padTLSPool in pad.go (see that comment for the
// full rationale): SoftClip is a one-shot function, so pooling avoids a
// fresh NewTLS per call, and putSoftClipTLS deliberately skips
// FreePseudostackTLS so opus_pcm_soft_clip's own lazily-cached pseudostack
// scratch buffer survives across borrows instead of being rebuilt every
// call. This is a separate pool from pad.go's because it caches TLS
// instances used with opuscc (decoder-side) transpiled code rather than
// opusccenc (encoder-side) - keeping them apart avoids coupling the two
// independently generated packages' pseudostack layouts together.
var softClipTLSPool = sync.Pool{
	New: func() any { return libc.NewTLS() },
}

func getSoftClipTLS() (*libc.TLS, error) {
	tls, _ := softClipTLSPool.Get().(*libc.TLS)
	if tls == nil {
		return nil, errors.New("opus: failed to allocate TLS")
	}
	return tls, nil
}

func putSoftClipTLS(tls *libc.TLS) {
	if tls == nil {
		return
	}
	tls.Reset()
	softClipTLSPool.Put(tls)
}

// SoftClip is a stateless helper that applies soft clipping to a single buffer of float32 PCM samples in-place.
func SoftClip(pcm []float32, channels int) error {
	if channels <= 0 {
		return errors.New("opus: invalid channel count")
	}
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm)%channels != 0 {
		return fmt.Errorf("opus: pcm sample count %d must be multiple of channels %d", len(pcm), channels)
	}
	tls, err := getSoftClipTLS()
	if err != nil {
		return err
	}
	defer putSoftClipTLS(tls)

	mem := make([]float32, channels)
	nbSamples := int32(len(pcm) / channels)
	dataPtr := libc.PtrFloat32(pcm)
	memPtr := libc.PtrFloat32(mem)

	opuscc.Opus_opus_pcm_soft_clip(tls, dataPtr, nbSamples, int32(channels), memPtr)
	return nil
}
