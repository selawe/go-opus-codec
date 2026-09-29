package opus

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

const stackChildEnv = "OPUS_STACK_PACKET_CHILD"

// The decoder hands caller memory to transpiled code as a raw uintptr. If the
// caller's packet lives on its goroutine stack and the stack grows during the
// call, that address goes stale. GODEBUG=efence=1 faults freed stacks, so the
// test re-runs itself with it to turn a silent stale read into a hard failure.
func TestDecodeStackResidentPacket(t *testing.T) {
	if os.Getenv(stackChildEnv) == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDecodeStackResidentPacket$", "-test.count=1")
		cmd.Env = append(os.Environ(), stackChildEnv+"=1", "GODEBUG=efence=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child failed: %v\n%s", err, tail(string(out), 2000))
		}
		return
	}

	enc, err := NewEncoder(48000, 2, ApplicationAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16((i * 37) % 2000)
	}
	pk := make([]byte, 1500)
	n, err := enc.Encode(pcm, 960, pk)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stackPkt [400]byte
			var stackPCM [960 * 2]int16
			var stackF32 [960 * 2]float32
			copy(stackPkt[:], pk[:n])
			d, err := NewDecoder(48000, 2)
			if err != nil {
				t.Error(err)
				return
			}
			defer d.Close()
			if r, err := d.Decode(stackPkt[:n], stackPCM[:], 960, false); err != nil || r != 960 {
				t.Errorf("Decode = %d, %v", r, err)
			}
			if r, err := d.DecodeF32(stackPkt[:n], stackF32[:], 960, false); err != nil || r != 960 {
				t.Errorf("DecodeF32 = %d, %v", r, err)
			}
		}()
	}
	wg.Wait()
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + strings.TrimSpace(s[len(s)-n:])
	}
	return s
}
