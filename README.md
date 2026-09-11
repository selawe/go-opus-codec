# go-opus-codec

[![Go Reference](https://pkg.go.dev/badge/github.com/selawe/go-opus-codec.svg)](https://pkg.go.dev/github.com/selawe/go-opus-codec)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.24-00ADD8?logo=go)](https://golang.org)
[![Pure Go](https://img.shields.io/badge/CGO__ENABLED-0_(Pure_Go)-success?logo=go)](https://golang.org)
[![RFC Conformance](https://img.shields.io/badge/RFC_6716_%2F_RFC_8251-100%25_PASS_(120%2F120)-brightgreen)](conformance_matrix.md)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows%20%7C%20FreeBSD-blue)](#supported-platforms--architecture)
[![License](https://img.shields.io/badge/License-BSD_3--Clause-orange.svg)](LICENSE)

A pure Go implementation of an Ogg/Opus audio parser, decoder, encoder, and repacketizer without cgo (`CGO_ENABLED=0`). The underlying DSP codec logic was originally transpiled from reference libopus 1.6.1 C source using [ccgo](https://pkg.go.dev/modernc.org/ccgo/v4); subsequent development, extensions, and hardening are written purely in Go following the official IETF RFC specifications ([RFC 6716](https://tools.ietf.org/html/rfc6716), [RFC 8251](https://tools.ietf.org/html/rfc8251), [RFC 7845](https://tools.ietf.org/html/rfc7845), [RFC 3533](https://tools.ietf.org/html/rfc3533)).

---

> [!IMPORTANT]
> **Fork Attribution & Project Status**
>
> This project is an independent fork of [kazzmir/opus-go](https://github.com/kazzmir/opus-go) by Jon Rafkind, customized and tuned to support personal projects and specific audio pipeline requirements.
>
> **Key Modifications & Tuning in this Fork:**
> - **Full RFC Conformance**: RFC 6716 / RFC 8251 (TOC parsing, frame demuxing, LBRR/FEC detection, safe PLC, and `FinalRange` entropy range verification) and RFC 7845 (`OutputGainQ8`, header validations).
> - **Repacketizer API**: Seamless merging of multiple Opus packets (up to 120 ms) and sub-range extraction without re-encoding (`opus.Repacketizer`).
> - **CBR Packet Padding**: Constant Bitrate packet padding and unpadding for single-stream and multistream audio (`opus.PacketPad`, `opus.PacketUnpad`).
> - **Soft Clipping**: Non-linear dynamic range compression for float32 PCM (`opus.SoftClip`, `opus.SoftClipper`) to prevent harsh clipping distortion.
> - **Multichannel Surround**: Support for 5.1 and 7.1 surround sound audio playback and decoding.
> - **Concurrency Safety**: Full mutex synchronization across `opus.Decoder`, `opus.Encoder`, `opus.Repacketizer`, and `player.OpusPlayer`.
> - **Memory Safety**: Heap staging buffers and chunked block allocator in `libcshim` to eliminate pointer instability.
> - **Performance**: Slice-by-8 parallel CRC32 (~3.9x speedup), Ogg multi-packet page batching, and byte resynchronization.
> - **Float32 & Int16**: First-class support for both normalized float32 `[-1.0, 1.0]` and 16-bit linear PCM.
> - **Compile-Time Guard**: Rejection of 32-bit builds to prevent runtime pointer misalignment.

> [!WARNING]
> **Notice & Usage Disclaimer for Third Parties**
>
> If you plan to use this library in your own projects, please be aware of the following:
> 1. **Personal Project Focus**: This library is maintained primarily for personal project needs. APIs and internal behavior may evolve based on author requirements.
> 2. **Not a Drop-in Replacement**: Due to extensive modifications, refactoring, and module rename, this library is not guaranteed to remain API-compatible with upstream `kazzmir/opus-go`.
> 3. **Architecture Constraint**: Only **64-bit little-endian architectures** (`amd64`, `arm64`) are supported. 32-bit systems (`386`, `arm`, `wasm`) are intentionally blocked at compile time.
> 4. **License & As-Is**: Provided under the original 3-Clause BSD License without warranties of any kind. Use at your own risk.

---

## Table of Contents

- [Installation](#installation)
- [RFC 6716 / RFC 8251 Conformance Matrix](#rfc-6716--rfc-8251-conformance-matrix)
- [Supported Platforms & Architecture](#supported-platforms--architecture)
- [Quick Start Recipes](#quick-start-recipes)
  - [1. High-Level Audio Streaming & Playback](#1-high-level-audio-streaming--playback)
  - [2. Low-Level Packet Decoding & Packet Loss Concealment (PLC)](#2-low-level-packet-decoding--packet-loss-concealment-plc)
  - [3. Low-Level Audio Encoding (VBR, Complexity, In-Band FEC)](#3-low-level-audio-encoding-vbr-complexity-in-band-fec)
  - [4. RTP / WebRTC Frame Merging with Repacketizer](#4-rtp--webrtc-frame-merging-with-repacketizer)
  - [5. CBR Packet Padding & Unpadding](#5-cbr-packet-padding--unpadding)
  - [6. Float32 Dynamic Range Soft Clipping](#6-float32-dynamic-range-soft-clipping)
  - [7. Muxing & Demuxing Ogg Opus Containers](#7-muxing--demuxing-ogg-opus-containers)
- [Examples Directory](#examples-directory)
- [Command Line Utilities](#command-line-utilities)
- [Testing & Conformance](#testing--conformance)
- [Benchmarks](#benchmarks)
- [License](#license)

---

## Installation

```sh
go get github.com/selawe/go-opus-codec
```

Requires Go 1.24 or higher. No C compiler, headers, or cgo needed (`CGO_ENABLED=0`).

---

## RFC 6716 / RFC 8251 Conformance Matrix

`go-opus-codec` includes an automated 120-scenario conformance test suite evaluating the decoder against all 12 official IETF test vectors across all standard sample rates and channel counts.

**Score: 120 / 120 Tests Passed (100.0%)**

```text
+----------+----+----+----+----+----+----+----+----+----+----+----+----+
| Rate     | Ch | 01 | 02 | 03 | 04 | 05 | 06 | 07 | 08 | 09 | 10 | 11 | 12 |
+----------+----+----+----+----+----+----+----+----+----+----+----+----+
| 8000     | 1  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 8000     | 2  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 12000    | 1  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 12000    | 2  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 16000    | 1  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 16000    | 2  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 24000    | 1  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 24000    | 2  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 48000    | 1  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
| 48000    | 2  | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK | OK |
+----------+----+----+----+----+----+----+----+----+----+----+----+----+
```

All 120 test cases run concurrently in **~4 seconds**. For detailed results, see [conformance_matrix.md](conformance_matrix.md).

---

## Supported Platforms & Architecture

Because `go-opus-codec` is 100% pure Go without cgo, cross-compilation is trivial and requires only setting standard Go environment variables (`GOOS` and `GOARCH`).

| Operating System | Architecture | Status | Target Environments |
|---|---|:---:|---|
| **Linux** | `amd64`, `arm64` | ✅ Supported | Servers, Containers, Raspberry Pi 4/5, AWS Graviton |
| **macOS (Darwin)** | `arm64`, `amd64` | ✅ Supported | Apple Silicon (M1-M4) & Intel Macs |
| **Windows** | `amd64`, `arm64` | ✅ Supported | Windows 10/11 x64, Windows on ARM |
| **FreeBSD** | `amd64` | ✅ Supported | FreeBSD 64-bit |
| **32-Bit Systems** (`386`, `arm`, `wasm`) | 32-bit | ⛔ Blocked | Blocked at compile time to prevent pointer misalignment |

> [!NOTE]
> **64-Bit Requirement**: The underlying transpiled libopus code requires 64-bit little-endian pointers (LP64). A compile-time guard (`const _ = uint(unsafe.Sizeof(uintptr(0))) - 8`) in `libcshim` rejects 32-bit builds cleanly during compilation.

---

## Quick Start Recipes

### 1. High-Level Audio Streaming & Playback
Stream and decode an `.opus` or `.ogg` file into 16-bit linear PCM (48 kHz) with seeking:

```go
package main

import (
    "fmt"
    "io"
    "log"
    "time"

    opusgo "github.com/selawe/go-opus-codec"
)

func main() {
    // Open Opus stream directly from file (or use NewPlayerFromReader for HTTP/network)
    player, err := opusgo.NewPlayerFromFile("music.opus", true)
    if err != nil {
        log.Fatalf("Failed to open player: %v", err)
    }
    defer player.Close()

    // Seek accurately by time
    _ = player.SeekTime(15 * time.Second)

    // Read decoded 48kHz s16le PCM samples
    pcmBuf := make([]byte, 48000*2*2) // 1 second buffer (48k * 2 ch * 2 bytes)
    for {
        n, err := player.Read(pcmBuf)
        if err != nil {
            if err == io.EOF {
                break
            }
            log.Fatalf("Read error: %v", err)
        }
        fmt.Printf("Decoded %d PCM bytes (current sample: %d)\n", n, player.CurrentSample())
    }
}
```

---

### 2. Low-Level Packet Decoding & Packet Loss Concealment (PLC)
Decode individual Opus packets directly from RTP/WebRTC or files, recovering dropped packets seamlessly:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    // Create decoder (48000 Hz, stereo)
    dec, err := opus.NewDecoder(48000, 2)
    if err != nil {
        log.Fatalf("NewDecoder failed: %v", err)
    }
    defer dec.Close()

    pcmOut := make([]int16, 960*2) // 20ms @ 48kHz stereo = 960 samples per channel

    // Case A: Decode normal incoming packet
    var receivedPacket []byte // received from UDP socket or container
    samples, err := dec.Decode(receivedPacket, pcmOut, 960, false)
    if err != nil {
        log.Fatalf("Decode failed: %v", err)
    }
    fmt.Printf("Decoded %d samples per channel\n", samples)

    // Case B: Packet was lost in transmission -> invoke Packet Loss Concealment (PLC)
    plcSamples, err := dec.Decode(nil, pcmOut, 960, false)
    if err != nil {
        log.Fatalf("PLC failed: %v", err)
    }
    fmt.Printf("PLC concealed %d replacement samples smoothly!\n", plcSamples)

    // Verify entropy range state
    finalRange, _ := dec.FinalRange()
    fmt.Printf("Decoder FinalRange: 0x%08x\n", finalRange)
}
```

---

### 3. Low-Level Audio Encoding (VBR, Complexity, In-Band FEC)
Encode PCM audio into compressed Opus packets with fine-grained encoder control:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    // Initialize encoder for VoIP or Audio
    enc, err := opus.NewEncoder(48000, 2, opus.ApplicationAudio)
    if err != nil {
        log.Fatalf("NewEncoder failed: %v", err)
    }
    defer enc.Close()

    // Fine-tune encoder parameters
    _ = enc.SetBitrate(64000)      // 64 kbps target
    _ = enc.SetVBR(true)           // Enable Variable Bitrate
    _ = enc.SetComplexity(10)      // Highest quality DSP mode (1..10)
    _ = enc.SetInbandFEC(true)     // Enable in-band forward error correction
    _ = enc.SetPacketLossPerc(15)  // Optimize for 15% expected network packet loss

    lookahead, _ := enc.Lookahead()
    fmt.Printf("Encoder lookahead: %d samples\n", lookahead)

    pcmIn := make([]int16, 960*2) // 20ms @ 48kHz stereo
    packetBuf := make([]byte, 1275)

    nBytes, err := enc.Encode(pcmIn, 960, packetBuf)
    if err != nil {
        log.Fatalf("Encode failed: %v", err)
    }

    packet := packetBuf[:nBytes]
    fmt.Printf("Encoded 20ms audio frame: %d bytes (TOC: 0x%02x)\n", len(packet), packet[0])
}
```

---

### 4. RTP / WebRTC Frame Merging with Repacketizer
Merge multiple 20ms packets into a single 60ms packet to cut UDP/IP header overhead by >60%:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    rp, err := opus.NewRepacketizer()
    if err != nil {
        log.Fatalf("NewRepacketizer failed: %v", err)
    }
    defer rp.Close()

    // Feed multiple consecutive 20ms packets
    var packet1, packet2, packet3 []byte // each is a 20ms Opus packet
    _ = rp.Cat(packet1)
    _ = rp.Cat(packet2)
    _ = rp.Cat(packet3)

    // Output a single merged 60ms Opus packet
    merged := make([]byte, 4000)
    mergedLen, err := rp.Out(merged)
    if err != nil {
        log.Fatalf("Repacketizer.Out failed: %v", err)
    }

    fmt.Printf("Merged %d frames into 1 packet (%d bytes)\n", rp.Frames(), mergedLen)

    // Sub-range extraction: extract frames 1 through 2 without re-encoding
    subPacket := make([]byte, 4000)
    subLen, _ := rp.OutRange(1, 3, subPacket)
    fmt.Printf("Extracted sub-packet: %d bytes\n", subLen)
}
```

---

### 5. CBR Packet Padding & Unpadding
Enforce constant packet lengths across network tunnels to defeat packet-size traffic analysis:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    var rawPacket []byte // Variable length packet (e.g. 185 bytes)

    // Pad packet up to exactly 500 bytes
    paddedPacket, err := opus.PacketPad(rawPacket, 500)
    if err != nil {
        log.Fatalf("PacketPad failed: %v", err)
    }
    fmt.Printf("Padded packet length: %d bytes\n", len(paddedPacket))

    // Strip padding on receiver side
    unpadded, err := opus.PacketUnpad(paddedPacket)
    if err != nil {
        log.Fatalf("PacketUnpad failed: %v", err)
    }
    fmt.Printf("Unpadded packet length: %d bytes (matches original)\n", len(unpadded))
}
```

---

### 6. Float32 Dynamic Range Soft Clipping
Smoothly compress hot audio peaks exceeding `[-1.0, 1.0]` before encoding to prevent digital distortion:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    // Float32 audio with peaks reaching +1.8
    pcm := make([]float32, 960*2)

    // Non-linear soft clipping in-place
    if err := opus.SoftClip(pcm, 2); err != nil {
        log.Fatalf("SoftClip failed: %v", err)
    }

    fmt.Println("Audio peaks smoothly compressed within [-1.0, 1.0] without harsh clipping!")
}
```

---

### 7. Muxing & Demuxing Ogg Opus Containers
Construct an RFC 7845 `.opus` file from scratch or parse metadata comments:

```go
package main

import (
    "bytes"
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/ogg"
)

func main() {
    var buf bytes.Buffer
    pw := ogg.NewPacketWriter(&buf, 0x12345678)

    // 1. Write OpusHead
    head := ogg.OpusHead{
        Version:         1,
        Channels:        2,
        PreSkip:         312,
        InputSampleRate: 48000,
    }
    headPkt, _ := ogg.BuildOpusHeadPacket(head)
    _ = pw.WritePacket(headPkt, 0, true, false)

    // 2. Write OpusTags
    tags := ogg.OpusTags{
        Vendor:   "go-opus-codec",
        Comments: []string{"TITLE=My Track", "ARTIST=Selawé"},
    }
    tagsPkt, _ := ogg.BuildOpusTagsPacket(tags)
    _ = pw.WritePacket(tagsPkt, 0, false, false)
    _ = pw.Flush()

    // 3. Read back using OpusReader
    reader, err := ogg.NewOpusReader(&buf)
    if err != nil {
        log.Fatalf("NewOpusReader failed: %v", err)
    }
    fmt.Printf("Title : %s\n", reader.Tags.Get("TITLE"))
    fmt.Printf("Artist: %s\n", reader.Tags.Get("ARTIST"))
}
```

---

## Examples Directory

The repository includes complete, executable examples in the [`examples/`](examples/) directory. Each example contains its own `README.md` with full code explanation and expected output:

| Directory | Description | Dependencies | How to Run |
|---|---|:---:|---|
| [`examples/repacketizer`](examples/repacketizer) | Merging 20ms frames into 60ms packets & sub-range extraction for RTP/WebRTC | **Pure Go** | `cd examples/repacketizer && go run .` |
| [`examples/roundtrip_pcm`](examples/roundtrip_pcm) | End-to-end PCM encode/decode with SoftClip, encoder tuning, and PLC loss recovery | **Pure Go** | `cd examples/roundtrip_pcm && go run .` |
| [`examples/stream_mux`](examples/stream_mux) | Generating RFC 7845 Ogg Opus files from scratch & demuxing tags/audio | **Pure Go** | `cd examples/stream_mux && go run .` |
| [`examples/cbr_padding`](examples/cbr_padding) | Enforcing constant packet sizes via `PacketPad` and `PacketUnpad` | **Pure Go** | `cd examples/cbr_padding && go run .` |
| [`examples/convert`](examples/convert) | High-performance MP3 to Ogg Opus converter with resampler and progress bar | `go-mp3` | `cd examples/convert && go run . in.mp3 out.opus` |
| [`examples/decode`](examples/decode) | Throughput benchmarking of Opus file decoding to `io.Discard` | **Pure Go** | `cd examples/decode && go run . audio.opus` |
| [`examples/play`](examples/play) | Local audio playback with sample-accurate seeking (int16 & float32) | `oto/v3` | `cd examples/play && go run . audio.opus` |
| [`examples/net`](examples/net) | Real-time playback of remote HTTP/HTTPS Opus audio streams | `oto/v3` | `cd examples/net && go run . https://url/stream.opus` |

---

## Command Line Utilities

The repository provides several production-ready command line tools in `cmd/`:

- **Decode Ogg Opus to WAV**:
  ```sh
  go run ./cmd/oggopus2wav --out out.wav input.opus
  ```
- **Encode WAV to Ogg Opus**:
  ```sh
  go run ./cmd/wav2oggopus --bitrate 64000 --out out.opus input.wav
  ```
- **Inspect Ogg Pages & Opus Packets**:
  ```sh
  go run ./cmd/oggopusdump --no-crc input.opus
  ```
- **Extract Raw Length-Prefixed Packets**:
  ```sh
  go run ./cmd/oggopusextract --out packets.bin input.opus
  ```

---

## Testing & Conformance

### Standard Unit Tests & Race Detection
```bash
# Run all unit tests offline (< 1 sec)
go test -v ./...

# Run race detector
go test -race -gcflags=all=-d=checkptr=0 ./player ./opus

# Run static analysis
go vet -unsafeptr=false ./opus/... ./test/... ./wav/...
```

### Official RFC 6716 / RFC 8251 Conformance Suite
Run the 120-test matrix against official IETF test vectors:

```bash
# Automatically downloads official test vectors and runs the 120-scenario suite:
bash scripts/run_conformance.sh
```

---

## Benchmarks

`benchmarks/codec-comparison` is a separate Go module (same layout as `examples/*`, with a `replace` back to this module) that benchmarks this library's decoder and encoder against two reference implementations decoding/encoding the exact same input:

- **[libopus](https://opus-codec.org/) (C, via cgo)** — the reference implementation, wrapped by [`github.com/hraban/opus`](https://github.com/hraban/opus).
- **[pion/opus](https://github.com/pion/opus)** — an independent, decoder-only, pure-Go Opus implementation (decode only; it has no public encoder as of v0.1.0).

It is isolated in its own module specifically so the cgo dependency on `libopus-dev` never leaks into this repository's zero-dependency, `CGO_ENABLED=0` main module.

### Results

Measured on an AMD Ryzen 9 5900HX (`go test -bench=. -benchmem`), decoding real packets from `testvectors/testvector01.bit` / encoding synthetic 48kHz stereo 20ms frames at 64kbps, complexity 10:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Decode — go-opus-codec (this repo) | 114,593 | 25 | 0 |
| Decode — libopus (cgo) | 72,893 | 0 | 0 |
| Decode — pion/opus | 139,052 | 82 | 1 |
| Encode — go-opus-codec (this repo) | 257,554 | 40 | 0 |
| Encode — libopus (cgo) | 104,925 | 0 | 0 |

**Takeaways:**
- `go-opus-codec` decodes ~1.6x slower than libopus C, but ~1.2x faster than `pion/opus`.
- `go-opus-codec` encodes ~2.5x slower than libopus C (no pure-Go encoder exists in `pion/opus` to compare against).
- **Zero dynamic allocations in steady-state:** Per-call heap allocations on encode and decode hot-paths have been eliminated down to **0 allocs/op** by preventing variadic argument escaping in `libcshim.VaList`. Only initial 1-time buffer sizing is amortized over iterations (~25–40 B/op).
- `libcshim`'s pthread-TLS emulation used to lock+map-lookup a single well-known key (`opusPseudostackTLSKey`) on every access, even though every caller (`opus.Decoder`, `Encoder`, `Repacketizer`) already serializes access with its own mutex around the whole call. Removing that redundant locking (confirmed safe via `go test -race`, which stays clean) shaved off the CPU profile's `Xpthread_getspecific`/mutex overhead entirely and got decode from 119,603 → 114,593 ns/op (~3%).
- **This is not a like-for-like "C vs Go" comparison.** The transpile in this repo was generated with `-DOPUS_DISABLE_INTRINSICS -U__SSE__ -U__SSE2__ -U__SSE3__ -U__SSSE3__ -U__AVX__ -U__AVX2__` (see the header comment in `opuscc/common.go`), i.e. all SIMD intrinsics were disabled at transpile time. The system `libopus.so` linked via cgo is typically built **with** SIMD enabled. The gap above is partly "no-SIMD Go vs SIMD-enabled C," not purely a language/runtime difference.

### Running it yourself

```bash
cd benchmarks/codec-comparison
go test -tags nolibopusfile -bench=. -benchmem
```

`-tags nolibopusfile` excludes `hraban/opus`'s optional `libopusfile`-based streaming API, which this benchmark doesn't use and which most systems don't have installed (only `libopus-dev` is required, not `libopusfile-dev`). Requires `CGO_ENABLED=1` and `libopus-dev` (or equivalent) installed — e.g. `apt install libopus-dev` on Debian/Ubuntu.

---

## License

This project is released under the [3-Clause BSD License](LICENSE).
Transpiled libopus code is copyright Xiph.Org Foundation, Skype Limited, Octasic Inc., and other contributors.
