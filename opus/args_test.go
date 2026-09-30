package opus

import (
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
