package opus

import (
	"errors"
	libc "github.com/selawe/go-opus-codec/libcshim"
	"testing"
)

func encodeOne(t *testing.T) []byte {
	t.Helper()
	enc, err := NewEncoder(48000, 1, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	pkt := make([]byte, 4000)
	n, err := enc.Encode(make([]int16, 960), 960, pkt)
	if err != nil {
		t.Fatal(err)
	}
	return pkt[:n]
}

// Oversized input used to abort inside the transpiled code with "pseudostack overflow"
// and unwind the caller with a panic.
func TestPacketPadRejectsOversizedInput(t *testing.T) {
	pkt := encodeOne(t)
	ok, err := PacketPad(pkt, maxPadInput)
	if err != nil {
		t.Fatalf("pad to the limit: %v", err)
	}
	if _, err := PacketPad(ok, maxPadInput+1); err != nil {
		t.Fatalf("limit-sized input must still work: %v", err)
	}
	big := append(append([]byte(nil), ok...), make([]byte, 10)...)
	if _, err := PacketPad(big, len(big)+1); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("PacketPad err = %v, want ErrPacketTooLarge", err)
	}
	if _, err := PacketUnpad(big); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("PacketUnpad err = %v, want ErrPacketTooLarge", err)
	}
	if _, err := MultistreamPacketPad(big, len(big)+1, 1); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("MultistreamPacketPad err = %v, want ErrPacketTooLarge", err)
	}
	if _, err := MultistreamPacketUnpad(big, 1); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("MultistreamPacketUnpad err = %v, want ErrPacketTooLarge", err)
	}
}

func TestPacketPadRejectsHugeTarget(t *testing.T) {
	if _, err := PacketPad(encodeOne(t), 1<<40); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
}

func TestWithPadTLSConvertsPanicToError(t *testing.T) {
	err := withPadTLS(func(*libc.TLS) { panic("boom") })
	if err == nil {
		t.Fatal("expected an error")
	}
	// The pool must still hand out a working TLS afterwards.
	if _, err := PacketPad(encodeOne(t), 500); err != nil {
		t.Fatalf("pad after panic: %v", err)
	}
}
