package opus

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

const stressChildEnv = "OPUS_STACK_STRESS_CHILD"

// The transpiled codec keeps raw uintptr addresses. If any of them points into the Go stack
// and the stack grows while the call is running, the address goes stale. This test runs the
// decoder (SILK, CELT, hybrid, packet loss concealment) and the encoder from goroutines whose
// stacks are at every depth in a range, forcing stack growth in the middle of the calls, with a
// GC in between. Re-run with GODEBUG=efence=1, the runtime faults freed stacks, so a stale
// access crashes deterministically instead of silently reading old data. Results are also
// compared bit for bit with a shallow reference run, which catches silent corruption.
func TestStackGrowthStress(t *testing.T) {
	if os.Getenv(stressChildEnv) == "" {
		if runtime.GOOS != "linux" {
			t.Skip("GODEBUG=efence stack faulting is only exercised on linux")
		}
		if testing.Short() {
			t.Skip("stack growth stress is skipped in -short mode")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestStackGrowthStress$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), stressChildEnv+"=1", "GODEBUG=efence=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("stress run under GODEBUG=efence=1 failed: %v\n%s", err, tail(string(out), 3000))
		}
		return
	}

	pcm := make([]int16, 960*2)
	for i := range pcm {
		pcm[i] = int16((i*37)%4000 - 2000)
	}
	makePacket := func(app, channels, bitrate int) []byte {
		e, err := NewEncoder(48000, channels, app)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		if err := e.SetBitrate(bitrate); err != nil {
			t.Fatal(err)
		}
		pk := make([]byte, 1500)
		var n int
		for i := 0; i < 4; i++ { // let the encoder settle
			if n, err = e.Encode(pcm[:960*channels], 960, pk); err != nil {
				t.Fatal(err)
			}
		}
		return pk[:n]
	}

	type decodeCase struct {
		name     string
		channels int
		packet   []byte
	}
	decodes := []decodeCase{
		{"decode silk mono", 1, makePacket(ApplicationVoIP, 1, 16000)},
		{"decode celt stereo", 2, makePacket(ApplicationAudio, 2, 96000)},
		{"decode hybrid mono", 1, makePacket(ApplicationAudio, 1, 32000)},
	}
	decode := func(c decodeCase) []byte { // one packet, then packet loss concealment
		d, err := NewDecoder(48000, c.channels)
		if err != nil {
			panic(err)
		}
		defer d.Close()
		// The caller's buffers live on the goroutine stack too, the case a stale uintptr breaks.
		var stackPacket [1500]byte
		var stackPCM [960 * 2]int16
		var res bytes.Buffer
		for _, pk := range [][]byte{c.packet, nil} {
			var in []byte
			if pk != nil {
				in = stackPacket[:copy(stackPacket[:], pk)]
			}
			n, err := d.Decode(in, stackPCM[:960*c.channels], 960, false)
			if err != nil {
				panic(err)
			}
			fmt.Fprintf(&res, "%d:%v;", n, stackPCM[:n*c.channels])
		}
		return res.Bytes()
	}

	type encodeCase struct {
		name              string
		app, channels, br int
	}
	encodes := []encodeCase{
		{"encode voip mono", ApplicationVoIP, 1, 16000},
		{"encode audio stereo", ApplicationAudio, 2, 96000},
	}
	encode := func(c encodeCase) []byte {
		e, err := NewEncoder(48000, c.channels, c.app)
		if err != nil {
			panic(err)
		}
		defer e.Close()
		if err := e.SetBitrate(c.br); err != nil {
			panic(err)
		}
		var stackPCM [960 * 2]int16
		var stackPacket [1500]byte
		copy(stackPCM[:], pcm)
		var res bytes.Buffer
		for i := 0; i < 2; i++ {
			n, err := e.Encode(stackPCM[:960*c.channels], 960, stackPacket[:])
			if err != nil {
				panic(err)
			}
			res.Write(stackPacket[:n])

			// Exercise getters across stack movements
			br, _ := e.Bitrate()
			bw, _ := e.Bandwidth()
			sig, _ := e.Signal()
			comp, _ := e.Complexity()
			la, _ := e.Lookahead()
			fmt.Fprintf(&res, "g:%d,%d,%d,%d,%d;", br, int(bw), int(sig), comp, la)
		}
		return res.Bytes()
	}

	const depths, repsPerDepth = 48, 2
	run := func(name string, reference []byte, body func() []byte) {
		var wg sync.WaitGroup
		var moved, mismatched atomic.Int64
		var mu sync.Mutex
		var failures []string
		for depth := 0; depth < depths; depth++ {
			wg.Add(1)
			go func(depth int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						failures = append(failures, fmt.Sprintf("depth %d: panic: %v", depth, r))
						mu.Unlock()
					}
				}()
				for rep := 0; rep < repsPerDepth; rep++ {
					growStack(depth*3+rep, func() {
						runtime.GC()
						var marker int // stays on the stack; its address changes if the stack moves
						before := uintptr(unsafe.Pointer(&marker))
						got := body()
						if uintptr(unsafe.Pointer(&marker)) != before {
							moved.Add(1)
						}
						if !bytes.Equal(got, reference) {
							mismatched.Add(1)
						}
					})
				}
			}(depth)
		}
		wg.Wait()
		for _, f := range failures {
			t.Errorf("%s: %s", name, f)
		}
		if m := mismatched.Load(); m > 0 {
			t.Errorf("%s: %d runs differ from the shallow reference (stack growth corrupted the result)", name, m)
		}
		// Without at least one real stack move the run proves nothing.
		if moved.Load() == 0 {
			t.Errorf("%s: the goroutine stack never moved during a call; the stress test is vacuous", name)
		}
		t.Logf("%s: %d/%d calls had their stack moved mid-call", name, moved.Load(), depths*repsPerDepth)
	}

	for _, c := range decodes {
		run(c.name, decode(c), func() []byte { return decode(c) })
	}
	for _, c := range encodes {
		run(c.name, encode(c), func() []byte { return encode(c) })
	}
}

// growStack calls f from n frames deep, so the goroutine's stack is at a chosen depth (and near
// a different growth boundary) when f runs.
//
//go:noinline
func growStack(n int, f func()) {
	var pad [256]byte
	pad[n%len(pad)] = byte(n)
	if n > 0 {
		growStack(n-1, f)
	} else {
		f()
	}
	runtime.KeepAlive(&pad)
}
