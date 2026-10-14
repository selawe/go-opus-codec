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
> - **Any Input Sample Rate**: `EncodeWAVToOggOpus` and `wav2oggopus` accept WAV at any rate (8/12/16/24/48 kHz encoded natively, others such as 44.1 kHz resampled) using the streaming `resample` package, adapted from upstream [kazzmir/opus-go](https://github.com/kazzmir/opus-go).
> - **Float32 & Int16**: First-class support for both normalized float32 `[-1.0, 1.0]` and 16-bit linear PCM.
> - **More WAV Formats**: `wav.Reader` reads 8/24/32-bit integer and 32-bit float PCM (also `WAVE_FORMAT_EXTENSIBLE`) and RF64/BW64 files over 4 GiB, converting to int16 or float32; `wav.Writer` writes the extensible header above two channels.
> - **Chained & Multiplexed Ogg**: readers, `DecodeOggOpusToWAV` and `player` follow chained streams; files with several interleaved logical streams (Opus next to video, or one Opus stream per language) read the first Opus stream, with options to choose another.
> - **Context Cancellation**: `...Context` variants of every conversion stop promptly, leaving no partial files.
> - **OpusTags Editing**: `ogg.RewriteOpusTags` replaces comments without re-encoding, failing loudly on corrupt input.
> - **RTP & Downmix**: the `rtp` package (RFC 7587 packetizer/depacketizer with loss detection) and ITU-R BS.775 surround downmix routines.
> - **Hardening**: bounded work on hostile input (resampler tables, WAV rates, Ogg resync, output limits), a rewritten, correct `SeekToPage`, atomic output files, and panic recovery inside the codec (see [Security](#security)).
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
  - [8. Encoding Audio of Any Sample Rate & Resampling](#8-encoding-audio-of-any-sample-rate--resampling)
  - [9. Cancelling a Conversion with a Context](#9-cancelling-a-conversion-with-a-context)
  - [10. Editing OpusTags Without Re-encoding](#10-editing-opustags-without-re-encoding)
  - [11. Opus over RTP (RFC 7587)](#11-opus-over-rtp-rfc-7587)
  - [12. Surround Downmix to Stereo](#12-surround-downmix-to-stereo)
  - [13. Encoder & Decoder Controls](#13-encoder--decoder-controls)
  - [Multiplexed Ogg files](#multiplexed-ogg-files)
  - [Chained Ogg files](#chained-ogg-files)
- [Examples Directory](#examples-directory)
- [Command Line Utilities](#command-line-utilities)
- [Testing & Conformance](#testing--conformance)
  - [Fuzzing](#fuzzing)
- [Benchmarks](#benchmarks)
- [Security](#security)
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

> [!IMPORTANT]
> **Race detector / `checkptr`**: the transpiled libopus code addresses Go memory through `uintptr`, which Go's pointer checker (`-d=checkptr`, switched on automatically by `-race`) reports as `fatal error: checkptr: pointer arithmetic result points to invalid allocation`. That aborts the process on the first `Decoder`/`Encoder` call, so a plain `go test -race` of code that uses this library crashes. Disable only the pointer checker and keep the race detector:
>
> ```sh
> go test -race -gcflags=all=-d=checkptr=0 ./...
> ```
>
> `-race` itself keeps working: the public types are mutex-protected and the suite is race-clean under this flag. The flag is only needed when building with `-race` (or `-d=checkptr`) and can stay off for normal builds. See [Testing & Conformance](#standard-unit-tests--race-detection).

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

`Length()`, `TotalSamples()` and `TotalDuration()` can be called at any time on a seekable source without disturbing playback, and seeking decodes 80 ms of pre-roll (RFC 7845 Section 4.6) so audio right after `SeekTime` matches continuous playback. On a non-seekable stream those three calls read to the end and consume it.

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

    // 1. Write OpusHead and OpusTags. WriteOpusHeaders keeps the RFC 7845 page layout: OpusHead alone on the
    // first (BOS) page, OpusTags ending its own page, so audio always starts on a fresh page.
    err := pw.WriteOpusHeaders(
        ogg.OpusHead{Version: 1, Channels: 2, PreSkip: 312, InputSampleRate: 48000},
        ogg.OpusTags{
            Vendor:   "go-opus-codec",
            Comments: []string{"TITLE=My Track", "ARTIST=Selawé"},
        },
    )
    if err != nil {
        log.Fatalf("WriteOpusHeaders failed: %v", err)
    }

    // 2. Write the audio packets (here one placeholder; in practice opus.Encoder.Encode output) and flag the last
    // one as the end of the stream. Close writes any buffered page and flushes; it reports ogg.ErrMissingEOS if
    // the stream was never terminated.
    audio := []byte{0xFC, 0x00}
    if err := pw.WritePacket(audio, 312+960, false, true); err != nil {
        log.Fatalf("WritePacket failed: %v", err)
    }
    if err := pw.Close(); err != nil {
        log.Fatalf("Close failed: %v", err)
    }

    // 3. Read back using OpusReader
    reader, err := ogg.NewOpusReader(&buf)
    if err != nil {
        log.Fatalf("NewOpusReader failed: %v", err)
    }
    fmt.Printf("Title : %s\n", reader.Tags.Get("TITLE"))
    fmt.Printf("Artist: %s\n", reader.Tags.Get("ARTIST"))
}
```

Notes for writing and reading real streams:
- **Granule positions** (RFC 7845 Section 4) count every sample decoded so far, *pre-skip included*: page *n* of 20 ms frames carries `n*960`, not `PreSkip + n*960`. Only the last page is trimmed, to `PreSkip + input length`, and you must keep encoding silence after the input ends until the encoder lookahead (`enc.Lookahead()`) is flushed. See [`examples/stream_mux`](examples/stream_mux).
- **Page batching**: set `pw.MaxPageSize` (bytes) and `pw.MaxPagePackets` to group packets into pages instead of one page per packet (a 27+ byte header each). `WriteOpusHeaders` already ends the OpusTags page, so batched audio still starts on a fresh page; if you write the two header packets yourself, call `pw.FlushPage()` after each.
- **Ending the stream**: flag the last audio packet `eos=true` and call `pw.Close()`. Writes after an end-of-stream packet or after `Close` fail (`ErrWriteAfterEOS`, `ErrWriterClosed`), and the first write error is sticky.
- **Damaged input**: a stream cut inside a page returns `ogg.ErrTruncatedPage` from the page and packet readers (it matches `io.ErrUnexpectedEOF`, and still `io.EOF` for loops that only stop on EOF; `ogg.RewriteOpusTags` is stricter and does not match `io.EOF`). A gap in the page sequence drops the damaged packet instead of splicing it and sets `Discontinuity` on the next `OpusAudioPacket`, so you can conceal the loss with `DecodePacket(nil, ...)`.
- `OpusTags` may be up to `ogg.MaxOpusHeaderPacketSize` (16 MiB, for embedded cover art); audio packets are capped at `ogg.MaxOpusPacketSize` (64 KiB).

---

### 8. Encoding Audio of Any Sample Rate & Resampling
libopus only encodes 8, 12, 16, 24 and 48 kHz. The high-level helper handles everything else for you:

```go
in, _ := os.Open("cd_quality_44k1.wav") // any rate, mono or stereo; 8/16/24/32-bit integer or 32-bit float PCM
out, _ := os.Create("out.opus")
defer in.Close()
defer out.Close()

// 44.1 kHz is resampled to 48 kHz; OpusHead.InputSampleRate keeps the original 44100.
err := opusgo.EncodeWAVToOggOpus(in, out, &opusgo.EncodeOptions{Bitrate: 96000})
```

The decoded stream has exactly `ceil(frames * 48000 / rate)` samples per channel, and pre-skip and granule positions are counted at 48 kHz as RFC 7845 requires. Use `Encoder.PreSkip()` (not `Lookahead()`, which is in the encoder's own rate) when you write your own `OpusHead` for an encoder that does not run at 48 kHz.

To resample PCM yourself, `resample` is a streaming polyphase windowed-sinc converter (at least 40 dB alias rejection, passband within 0.1 dB up to 16 kHz, zero-phase so no extra pre-skip):

```go
import "github.com/selawe/go-opus-codec/resample"

r := resample.New(2, 44100, 48000) // channels, from, to (panics on invalid arguments: see Check below)
var out []int16
for chunk := range chunks {        // any chunking; interleaved 16-bit PCM
    out = append(out, r.ProcessInt16(chunk)...)
}
out = append(out, r.FlushInt16()...) // ends the stream; output length is exact
// or, for a whole buffer at once: resample.Int16(pcm, 2, 44100, 48000)
```

`resample.New` panics for non-positive arguments and for a rate pair whose filter table would be enormous (two large coprime rates need up to `resample.MaxTableEntries`, ~16 MB). When the rates come from untrusted input, such as a WAV header, call `resample.Check(channels, from, to)` first: it returns an error instead. `wav.NewReader` already rejects sample rates above `wav.MaxSampleRate` (768 kHz), and `EncodeWAVToOggOpus` runs `Check` itself.

---

### 9. Cancelling a Conversion with a Context
Every high-level conversion has a `...Context` variant that stops promptly when the context is cancelled or its deadline passes:

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"

    opusgo "github.com/selawe/go-opus-codec"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    err := opusgo.ConvertWAVFileToOggOpusContext(ctx, "in.wav", "out.opus", &opusgo.EncodeOptions{Bitrate: 64000})
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        log.Fatal("conversion took too long; no output file was left behind")
    case err != nil:
        log.Fatal(err)
    }
}
```

The context is checked before any file is opened, before each frame or packet, and before every read from the source, so it also stops a hostile Ogg file that skims through thousands of pages without yielding audio. A cancelled file conversion leaves neither the destination nor a temporary file. A `Read` that is already blocked inside the source (a stalled network reader) is not interrupted: close the source or give it a deadline. `EncodeWAVToOggOpusContext` and `DecodeOggOpusToWAVContext` take readers and writers instead of paths.

---

### 10. Editing OpusTags Without Re-encoding
`ogg.RewriteOpusTags` copies an Ogg Opus stream from a reader to a writer with new comment tags; audio packets, granule positions and the EOS flag are untouched (page numbers and CRCs are rewritten). It never edits in place, so write to a temporary file and rename it over the original:

```go
package main

import (
    "io"
    "log"
    "os"
    "path/filepath"

    "github.com/selawe/go-opus-codec/ogg"
)

func retag(path string) error {
    in, err := os.Open(path)
    if err != nil {
        return err
    }
    defer in.Close()

    r, err := ogg.NewOpusReader(in) // reads the current tags
    if err != nil {
        return err
    }
    tags := r.Tags
    tags.Set("TITLE", "New title") // replaces any existing TITLE
    tags.Add("COMMENT", "re-tagged")
    tags.Delete("ENCODER")
    if _, err := in.Seek(0, io.SeekStart); err != nil {
        return err
    }

    tmp, err := os.CreateTemp(filepath.Dir(path), ".retag-*")
    if err != nil {
        return err
    }
    defer os.Remove(tmp.Name()) // does nothing once the rename has happened
    if err := ogg.RewriteOpusTags(in, tmp, tags); err != nil {
        tmp.Close()
        return err
    }
    if err := tmp.Close(); err != nil {
        return err
    }
    in.Close() // Windows cannot rename over an open file
    return os.Rename(tmp.Name(), path)
}

func main() {
    if err := retag("song.opus"); err != nil {
        log.Fatal(err)
    }
}
```

`RewriteOpusTags` fails instead of losing data silently: a stream without OpusTags, a bad CRC or page-sequence gap, a truncated page and a stream that ends without an EOS page are all errors (a truncation error matches `io.ErrUnexpectedEOF`, not `io.EOF`). The new tags are checked first with `ogg.ValidateOpusTags` (RFC 7845 comment syntax, UTF-8, size) and nothing is written for malformed ones. In a multiplexed file the tags of the first Opus stream are rewritten and the other streams' pages are copied unchanged; in a chained file only the first link's tags are rewritten.

---

### 11. Opus over RTP (RFC 7587)
The `rtp` package turns Opus packets into RTP packets and back, with the 48 kHz timestamp clock RFC 7587 requires whatever the codec rate, and reports lost packets so the receiver can conceal them:

```go
package main

import (
    "errors"
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
    "github.com/selawe/go-opus-codec/rtp"
)

func main() {
    // Sender: SSRC, first sequence number and first timestamp are random, as RFC 3550 recommends.
    pz, err := rtp.NewRandomPacketizer(111) // 111: a dynamic payload type for Opus
    if err != nil {
        log.Fatal(err)
    }
    enc, _ := opus.NewEncoder(48000, 2, opus.ApplicationVoIP)
    defer enc.Close()
    frame := make([]byte, 1275)
    n, _ := enc.Encode(make([]int16, 960*2), 960, frame) // 20 ms of silence
    pkt, err := pz.Packetize(frame[:n])                  // pz.PacketizeMarker(frame[:n], true) after a DTX pause
    if err != nil {
        log.Fatal(err)
    }
    wire, err := pkt.Marshal() // send over UDP
    if err != nil {
        log.Fatal(err)
    }

    // Receiver.
    var in rtp.Packet
    if err := in.Unmarshal(wire); err != nil {
        log.Fatal(err)
    }
    dp := rtp.NewDepacketizer()
    payload, missing, err := dp.Depacketize(&in)
    if errors.Is(err, rtp.ErrDuplicatePacket) || errors.Is(err, rtp.ErrLatePacket) {
        return // drop it: decoding would repeat audio or play it out of order
    } else if err != nil {
        log.Fatal(err)
    }
    dec, _ := opus.NewDecoder(48000, 2)
    defer dec.Close()
    pcm := make([]int16, 960*2)
    for i := 0; i < missing; i++ {
        dec.Decode(nil, pcm, 960, false) // packet loss concealment for each lost packet
    }
    frames, _ := dec.Decode(payload, pcm, 960, false)
    fmt.Println("decoded frames:", frames, "missing before it:", missing)
}
```

Notes: `Packetize` refuses payloads above `Packetizer.MaxPayloadSize` (default `rtp.DefaultMaxPayloadSize`, 1200 bytes) and invalid payload types before it uses up a sequence number. Call `SkipTimestamp` for time in which nothing was sent (DTX), and set the marker bit on the first packet afterwards. `Packet.Marshal` returns an error for what it cannot write (extension headers, padding, more than 15 CSRCs). `Unmarshal` validates the padding count and its `Payload` aliases the input buffer. The `Depacketizer` expects packets in order (put a jitter buffer in front); `Packetizer` and `Depacketizer` are not safe for concurrent use.

---

### 12. Surround Downmix to Stereo
`opus.Downmix51ToStereo`, `Downmix71ToStereo` and `DownmixStereoToMono` (each with an `F32` variant) follow ITU-R BS.775 and expect the **Opus/Vorbis channel order** (5.1: FL FC FR RL RR LFE; 7.1: FL FC FR SL SR RL RR LFE), which is what the multichannel decoder produces. They saturate when every channel is loud; the `...Normalized` variants scale so that a full signal keeps its level:

```go
package main

import (
    "fmt"
    "log"

    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    // One 5.1 frame in Opus/Vorbis order: FL FC FR RL RR LFE.
    surround := []int16{8000, 6000, 8000, 4000, 4000, 12000}
    stereo, err := opus.Downmix51ToStereoNormalized(nil, surround)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("L, R:", stereo) // the LFE is not mixed in

    // Samples from a WAV file are in Microsoft/SMPTE order (FL FR FC LFE RL RR): reorder first.
    wavFrame := []int16{8000, 8000, 6000, 12000, 4000, 4000}
    vorbis, err := opus.ConvertSMPTEToVorbis51(nil, wavFrame) // ConvertSMPTEToVorbis71 for 7.1
    if err != nil {
        log.Fatal(err)
    }
    stereo, _ = opus.Downmix51ToStereoNormalized(stereo[:0], vorbis) // dst may be reused; src[:0] converts in place
    fmt.Println("L, R from the WAV frame:", stereo)
}
```

---

### 13. Encoder & Decoder Controls
Beyond bitrate, VBR and complexity, `opus.Encoder` exposes the signal-type hint (`SetSignal(opus.SignalVoice / SignalMusic / SignalAuto)`), `SetBandwidth` and `SetMaxBandwidth`, `SetForceChannels`, `SetLSBDepth`, `SetPredictionDisabled`, `SetPhaseInversionDisabled` and `SetVBRConstraint`, with a getter for each (`Bitrate`, `Bandwidth`, `Signal`, `Complexity`, ...). `Encoder.Bandwidth()` is the bandwidth of the last encoded frame, not the last value passed to `SetBandwidth`. `opus.Decoder` has `Bandwidth`, `LastPacketDuration` (including concealed frames), `Gain`, `Pitch` and `SetPhaseInversionDisabled`.

Every method of a closed `Encoder`, `Decoder`, `Repacketizer` or `SoftClipper` returns an error that matches `opus.ErrClosed` (`errors.Is`), and so does one whose codec hit an internal error: a panic inside the transpiled code is recovered into an error and disables that object. `EncodeF32` clamps its input to +/-16 and treats NaN as 0, because the transpiled encoder turns far larger values into silence.

---

### Multiplexed Ogg files

An Ogg file can interleave several logical streams (Opus next to a Theora video or a Vorbis track, or one Opus
stream per language). `ogg.OpusReader`, `DecodeOggOpusToWAV` and the players read the **first Opus stream** and skip
the pages of the others. To read another one, pass an option:

```go
r, err := ogg.NewOpusReaderWithOptions(f, ogg.WithSerial(0x1234))         // by serial number
r, err := ogg.NewOpusReaderWithOptions(f, ogg.WithStreamSelector(accept)) // by looking at the stream's first packet
p, err := opusgo.NewPlayerFromFile("talk.ogg", true, ogg.WithSerial(0x1234))
```

A file with no Opus stream at all fails with `ogg.ErrNotOpusOgg`. The raw `ogg.PacketReader` keeps the strict
behaviour (a second serial is an error) unless you call `SelectStream` or `SelectSerial`.

### Chained Ogg files

An Ogg Opus file can hold several logical bitstreams one after another (RFC 7845, for example
recordings joined with `cat a.opus b.opus`). `ogg.PacketReader`, `ogg.OpusReader` and
`DecodeOggOpusToWAV` follow such chains: `OpusReader.ReadAudioPacket` updates `Head` and `Tags` at each
boundary and marks the first packet of a new stream with `NewStream` and `StreamIndex`. The decoder
converts a chain whose streams all have the same channel count, and fails with an error on a
mismatch or on more than 65536 streams.

`player.OpusPlayer` plays a chain through as well (a stream with a different channel count ends playback with
`player.ErrStreamChange`). The limit to know about: seeking (`SeekToPage`, the player's `Seek*`), `Length` and
`TotalSamples` assume a single stream, so on a chain they can land in the wrong stream or report only the last
stream's length. Read chained files sequentially.

## Examples Directory

The repository includes complete, executable examples in the [`examples/`](examples/) directory. Each example contains its own `README.md` with full code explanation and expected output:

| Directory | Description | Dependencies | How to Run |
|---|---|:---:|---|
| [`examples/repacketizer`](examples/repacketizer) | Merging 20ms frames into 60ms packets & sub-range extraction for RTP/WebRTC | **Pure Go** | `cd examples/repacketizer && go run .` |
| [`examples/roundtrip_pcm`](examples/roundtrip_pcm) | End-to-end PCM encode/decode with SoftClip, encoder tuning, and PLC loss recovery | **Pure Go** | `cd examples/roundtrip_pcm && go run .` |
| [`examples/stream_mux`](examples/stream_mux) | Generating RFC 7845 Ogg Opus files from scratch & demuxing tags/audio | **Pure Go** | `cd examples/stream_mux && go run .` |
| [`examples/cbr_padding`](examples/cbr_padding) | Enforcing constant packet sizes via `PacketPad` and `PacketUnpad` | **Pure Go** | `cd examples/cbr_padding && go run .` |
| [`examples/convert`](examples/convert) | High-performance MP3 to Ogg Opus converter with the `resample` package and progress bar | `go-mp3` | `cd examples/convert && go run . in.mp3 out.opus` |
| [`examples/decode`](examples/decode) | Throughput benchmarking of Opus file decoding to `io.Discard` | **Pure Go** | `cd examples/decode && go run . audio.opus` |
| [`examples/play`](examples/play) | Local audio playback with sample-accurate seeking (int16 & float32) | `oto/v3` | `cd examples/play && go run . audio.opus` (`-int16` for integer output) |
| [`examples/net`](examples/net) | Real-time playback of remote HTTP/HTTPS Opus audio streams | `oto/v3` | `cd examples/net && go run . https://url/stream.opus` |

---

## Command Line Utilities

The repository provides several production-ready command line tools in `cmd/`:

- **Decode Ogg Opus to WAV**:
  ```sh
  go run ./cmd/oggopus2wav --out out.wav input.opus
  ```
  Decoding stops with an error once the PCM exceeds `--max-bytes` (default 1 GiB), so a small hostile file cannot fill the disk. The library functions apply no limit by default: when decoding untrusted input call `DecodeOggOpusToWAV` / `ConvertOggOpusFileToWAV` with `opusgo.WithMaxOutputBytes(opusgo.DefaultMaxOutputBytes)`, because an Opus packet of a few bytes can decode to 120 ms of audio.
- **Encode WAV to Ogg Opus** (any sample rate, mono or stereo; 8/16/24/32-bit integer PCM or 32-bit float, including RF64; non-libopus rates such as 44.1 kHz are resampled to 48 kHz):
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

**Output safety.** `oggopus2wav`, `wav2oggopus` and `oggopusextract` (and `ConvertWAVFileToOggOpus` / `ConvertOggOpusFileToWAV`) write through a temporary file that only replaces the destination once the conversion succeeded. A failed run (or a crash or kill part-way) therefore never leaves a partial file behind and never destroys an existing output, and writing onto the input file, or onto a read-only file, is refused with an error. A symlink destination keeps being a symlink and its target is the file that is replaced. Special files such as `/dev/stdout` are written in place. Ctrl-C or SIGTERM discards the temporary file and exits with status 130 or 143 instead of leaving it behind. Encoded files batch packets into Ogg pages of about one second, keep OpusTags on their own page, and carry RFC 7845 granule positions, so they play back with the exact input length in libopus-based players (verified with ffmpeg).

---

## Testing & Conformance

### Standard Unit Tests & Race Detection
```bash
# Run all unit tests offline (a few seconds)
go test ./...

# Run race detector. -d=checkptr=0 is required (see the note under "Supported Platforms"):
# the transpiled codec passes Go memory as uintptr, which -race's checkptr rejects.
go test -race -gcflags=all=-d=checkptr=0 . ./ogg ./wav ./player ./test ./opus ./resample ./rtp ./internal/...

# Static analysis (the transpiled opuscc/opusccenc packages are excluded)
make vet
make lint      # golangci-lint, configured in .golangci.yml
```

The Makefile wraps these: `make test`, `make test-cgo0` (`CGO_ENABLED=0`), `make test-race`, `make cross-compile`, `make build-examples` and `make conformance` (download, verify and run the RFC vectors).

### Fuzzing
The parsers and the decoder have fuzz targets (`ogg`, `wav`, `opus`, `resample`). Run one locally with, for example:

```bash
go test -run '^$' -fuzz '^FuzzOpusReaderNoCRC$' -fuzztime 30s ./ogg
```

`-fuzz` is a regexp, so anchor the name: `FuzzDecode` alone also matches `FuzzDecodeF32` and `go test` refuses to fuzz. A scheduled workflow (`.github/workflows/fuzz.yml`) runs every target nightly and uploads crashing inputs as artifacts.

> [!TIP]
> Without `-gcflags=all=-d=checkptr=0`, `go test -race` fails immediately with `checkptr: pointer arithmetic result points to invalid allocation`. That is expected for this library and not a data race. The same applies to your own tests that import `opus` or `player`.

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

Measured on an AMD Ryzen 9 5900HX under WSL2 with Go 1.27.1 (median of 5 runs of `go test -tags nolibopusfile -bench 'Benchmark(Encode|Decode)_(GoOpusCodec|Libopus|PionOpus)$' -benchmem -benchtime=2s -count=5`), decoding real packets from `testvectors/testvector01.bit` / encoding synthetic 48kHz stereo 20ms frames at 64kbps, complexity 10:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Decode — go-opus-codec (this repo) | 79,748 | 0 | 0 |
| Decode — libopus (cgo) | 47,237 | 0 | 0 |
| Decode — pion/opus | 89,589 | 205 | 0 |
| Encode — go-opus-codec (this repo) | 308,675 | 0 | 0 |
| Encode — libopus (cgo) | 143,125 | 0 | 0 |

**Takeaways:**
- `go-opus-codec` decodes ~1.7x slower than libopus C, but ~1.1x faster than `pion/opus`.
- `go-opus-codec` encodes ~2.2x slower than libopus C (no pure-Go encoder exists in `pion/opus` to compare against).
- **True zero-allocation in steady-state (0 B/op, 0 allocs/op):** Per-call heap allocations on encode and decode hot-paths have been completely eliminated down to **0 B/op and 0 allocs/op** by preventing variadic argument escaping in `libcshim.VaList` and isolating one-time lazy buffer initialization before `b.ResetTimer()`.
- `libcshim`'s pthread-TLS emulation used to lock+map-lookup a single well-known key (`opusPseudostackTLSKey`) on every access, even though every caller (`opus.Decoder`, `Encoder`, `Repacketizer`) already serializes access with its own mutex around the whole call. Removing that redundant locking (confirmed safe via `go test -race -gcflags=all=-d=checkptr=0`, which stays clean) shaved off the CPU profile's `Xpthread_getspecific`/mutex overhead entirely and got decode from 119,603 → 114,593 ns/op (~3%).
- **This is not a like-for-like "C vs Go" comparison.** The transpile in this repo was generated with `-DOPUS_DISABLE_INTRINSICS -U__SSE__ -U__SSE2__ -U__SSE3__ -U__SSSE3__ -U__AVX__ -U__AVX2__` (see the header comment in `opuscc/common.go`), i.e. all SIMD intrinsics were disabled at transpile time. The system `libopus.so` linked via cgo is typically built **with** SIMD enabled. The gap above is partly "no-SIMD Go vs SIMD-enabled C," not purely a language/runtime difference.

### Running it yourself

```bash
cd benchmarks/codec-comparison
go test -tags nolibopusfile -bench=. -benchmem
```

`-tags nolibopusfile` excludes `hraban/opus`'s optional `libopusfile`-based streaming API, which this benchmark doesn't use and which most systems don't have installed (only `libopus-dev` is required, not `libopusfile-dev`). Requires `CGO_ENABLED=1` and `libopus-dev` (or equivalent) installed — e.g. `apt install libopus-dev` on Debian/Ubuntu.

---

## Security

The library parses untrusted input (Ogg, Opus packets, WAV headers). Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md). Things to know when you accept files from others:

- Decoding to WAV: pass `opusgo.WithMaxOutputBytes(opusgo.DefaultMaxOutputBytes)`; a few bytes of Opus can decode to 120 ms of audio.
- `wav.Reader` rejects sample rates above 768 kHz and `resample.Check` bounds the resampler's filter table.
- The Ogg readers cap packet sizes (`SetMaxPacketSize`) and bound the work done while resynchronizing over damaged bytes. `DecodeOggOpusToWAV` follows at most 65536 chained streams and returns `opusgo.ErrTooManyStreams` beyond that.
- `RewriteOpusTags` and the file conversions write through a temporary file and fail without touching the original.

---

## License

This project is released under the [3-Clause BSD License](LICENSE).
Transpiled libopus code is copyright Xiph.Org Foundation, Skype Limited, Octasic Inc., and other contributors; its licence text, and the notices for other adapted code, are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and must accompany redistributions.
