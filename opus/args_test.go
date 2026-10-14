package opus

import (
	"errors"
	"math"
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
