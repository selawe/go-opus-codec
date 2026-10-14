package opus

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestSettersRejectValuesThatWouldWrapInt32(t *testing.T) {
	enc, err := NewEncoder(48000, 1, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if err := enc.SetBitrate(1<<32 + 64000); err == nil {
		t.Error("SetBitrate accepted a value that wraps to 64000")
	}
	if err := enc.SetComplexity(1<<32 + 5); err == nil {
		t.Error("SetComplexity accepted a value that wraps to 5")
	}
	if err := enc.SetBitrate(64000); err != nil {
		t.Errorf("SetBitrate(64000): %v", err)
	}

	dec, err := NewDecoder(48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	if err := dec.SetGain(1<<32 + 256); err == nil {
		t.Error("SetGain accepted a value that wraps to 256")
	}
}

func TestDecoderSetLastFrameSizeIsClampedAndResetRestoresDefault(t *testing.T) {
	dec, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	dec.SetLastFrameSize(1 << 40)
	if got := dec.LastFrameSize(); got != 5760 {
		t.Fatalf("LastFrameSize = %d, want clamp to 5760", got)
	}
	if err := dec.Reset(); err != nil {
		t.Fatal(err)
	}
	if got := dec.LastFrameSize(); got != 960 {
		t.Fatalf("LastFrameSize after Reset = %d, want 960", got)
	}
}

func TestDecodePacketOnNilDecoderReturnsError(t *testing.T) {
	var d *Decoder
	if _, _, err := d.DecodePacket(nil, nil); err == nil {
		t.Error("DecodePacket on a nil decoder must return an error")
	}
	if _, _, err := d.DecodePacketF32(nil, nil); err == nil {
		t.Error("DecodePacketF32 on a nil decoder must return an error")
	}
	if _, _, err := d.DecodePacketFEC(nil, nil); err == nil {
		t.Error("DecodePacketFEC on a nil decoder must return an error")
	}
}

// Float input far beyond full scale used to make the encoder emit silence without an error.
func TestEncodeF32ExtremeAmplitudeDoesNotGoSilent(t *testing.T) {
	for _, amp := range []float64{1, 1000, 1e4, 1e6, 1e30, math.Inf(1), math.NaN()} {
		enc, err := NewEncoder(48000, 1, ApplicationAudio)
		if err != nil {
			t.Fatal(err)
		}
		dec, err := NewDecoder(48000, 1)
		if err != nil {
			t.Fatal(err)
		}
		pcm := make([]float32, 960)
		out := make([]float32, 960)
		pkt := make([]byte, 4000)
		peak := float32(0)
		for f := 0; f < 10; f++ {
			for i := range pcm {
				v := math.Sin(2*math.Pi*440*float64(f*960+i)/48000) * amp
				if math.IsNaN(amp) {
					v = math.NaN()
				} else if math.IsInf(amp, 0) {
					v = math.Inf(1)
				}
				pcm[i] = float32(v)
			}
			n, err := enc.EncodeF32(pcm, 960, pkt)
			if err != nil {
				t.Fatal(err)
			}
			m, err := dec.DecodeF32(pkt[:n], out, 960, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range out[:m] {
				peak = max(peak, v)
			}
		}
		if math.IsNaN(amp) {
			// NaN becomes silence by design; it must simply not break the encoder.
			if peak != peak || peak > 0.01 {
				t.Errorf("NaN input: peak %v, want ~0", peak)
			}
		} else if peak < 0.5 {
			t.Errorf("amplitude %g decoded to silence (peak %v)", amp, peak)
		}
		enc.Close()
		dec.Close()
	}
}

func TestConstructorsRejectValuesThatWrapInt32(t *testing.T) {
	if d, err := NewDecoder(1<<32+48000, 1); err == nil {
		d.Close()
		t.Error("NewDecoder accepted a sample rate that wraps to 48000")
	}
	if d, err := NewDecoder(48000, 1<<32+2); err == nil {
		d.Close()
		t.Error("NewDecoder accepted a channel count that wraps to 2")
	}
	if e, err := NewEncoder(1<<32+48000, 1, ApplicationAudio); err == nil {
		e.Close()
		t.Error("NewEncoder accepted a sample rate that wraps to 48000")
	}
	if e, err := NewEncoder(48000, 1, 1<<32+ApplicationAudio); err == nil {
		e.Close()
		t.Error("NewEncoder accepted an application that wraps to a valid one")
	}
	d, err := NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("a valid decoder must still work: %v", err)
	}
	d.Close()
}

func TestPacketPadRejectsLargeTargetBeforeAllocating(t *testing.T) {
	pkt := encodeOne(t)
	if _, err := PacketPad(pkt, maxPadInput+1); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
	if _, err := PacketPad(pkt, 1<<30); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("1 GiB target: err = %v, want ErrPacketTooLarge", err)
	}
	if out, err := PacketPad(pkt, maxPadInput); err != nil || len(out) != maxPadInput {
		t.Fatalf("pad to the limit: len=%d err=%v", len(out), err)
	}
}

// A panic inside the transpiled codec must come back as an error and disable the object, not
// unwind the caller or leave a half-updated instance that still looks usable. A tiny bogus state
// pointer makes the codec dereference an invalid address, which Go raises as a recoverable panic.
func TestCodecPanicBecomesErrorAndDisablesObject(t *testing.T) {
	pkt := encodeOne(t)

	t.Run("decoder", func(t *testing.T) {
		for _, f32 := range []bool{false, true} {
			dec, err := NewDecoder(48000, 1)
			if err != nil {
				t.Fatal(err)
			}
			dec.st = 16
			call := func() error {
				if f32 {
					_, err := dec.DecodeF32(pkt, make([]float32, 960), 960, false)
					return err
				}
				_, err := dec.Decode(pkt, make([]int16, 960), 960, false)
				return err
			}
			if err := call(); err == nil || !strings.Contains(err.Error(), "internal codec error") {
				t.Fatalf("f32=%v: first call err = %v, want an internal codec error", f32, err)
			}
			if err := call(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("f32=%v: later call err = %v, want it to report closed", f32, err)
			}
			if err := dec.Close(); err != nil {
				t.Fatalf("Close after a panic: %v", err)
			}
		}
	})

	t.Run("encoder", func(t *testing.T) {
		for _, f32 := range []bool{false, true} {
			enc, err := NewEncoder(48000, 1, ApplicationAudio)
			if err != nil {
				t.Fatal(err)
			}
			enc.st = 16
			out := make([]byte, 4000)
			call := func() error {
				if f32 {
					_, err := enc.EncodeF32(make([]float32, 960), 960, out)
					return err
				}
				_, err := enc.Encode(make([]int16, 960), 960, out)
				return err
			}
			if err := call(); err == nil || !strings.Contains(err.Error(), "internal codec error") {
				t.Fatalf("f32=%v: first call err = %v, want an internal codec error", f32, err)
			}
			if err := call(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("f32=%v: later call err = %v, want it to report closed", f32, err)
			}
			if err := enc.Close(); err != nil {
				t.Fatalf("Close after a panic: %v", err)
			}
		}
	})
}

// libopus applies SET_FORCE_CHANNELS(2) to every sub-encoder; a mono one rejects it, which used to
// leave a multistream encoder half-changed. It must be refused before anything is touched.
func TestSetForceChannelsMultistreamIsAllOrNothing(t *testing.T) {
	// 3 channels as one coupled (stereo) stream plus one mono stream.
	enc, err := NewMultistreamEncoder(48000, 3, 2, 1, []uint8{0, 1, 2}, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	before, err := enc.ForceChannels()
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.SetForceChannels(2); err == nil {
		t.Fatal("forcing stereo on an encoder with a mono stream must fail")
	}
	if after, err := enc.ForceChannels(); err != nil || after != before {
		t.Fatalf("ForceChannels changed from %d to %d (err %v) although the call failed", before, after, err)
	}
	if err := enc.SetForceChannels(1); err != nil {
		t.Fatalf("forcing mono is valid everywhere: %v", err)
	}

	// All streams coupled: stereo is valid for every sub-encoder.
	st, err := NewMultistreamEncoder(48000, 4, 2, 2, []uint8{0, 1, 2, 3}, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetForceChannels(2); err != nil {
		t.Fatalf("all-stereo multistream: %v", err)
	}

	var nilEnc *Encoder
	if err := nilEnc.SetForceChannels(1); err == nil {
		t.Fatal("nil encoder must return an error, not panic")
	}
}
