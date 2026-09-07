# go-opus-codec

[![Go Reference](https://pkg.go.dev/badge/github.com/selawe/go-opus-codec.svg)](https://pkg.go.dev/github.com/selawe/go-opus-codec)

A pure Go implementation of an Ogg/Opus audio parser, decoder, and encoder without cgo (`CGO_ENABLED=0`). The underlying DSP codec logic was originally transpiled from reference libopus 1.6.1 C source using [ccgo](https://pkg.go.dev/modernc.org/ccgo/v4); subsequent development, extensions, and hardening are written purely in Go following the official IETF RFC specifications (RFC 6716, RFC 8251, RFC 7845, RFC 3533).

> [!IMPORTANT]
> **Fork Attribution & Project Status**
>
> This project is an independent fork of [kazzmir/opus-go](https://github.com/kazzmir/opus-go) by Jon Rafkind, customized and tuned to support personal projects and specific audio pipeline requirements.
>
> **Key Modifications & Tuning in this Fork:**
> - **Full RFC Conformance**: RFC 6716 / RFC 8251 (TOC parsing, frame demuxing, LBRR/FEC detection, safe PLC, and FinalRange entropy range checking) and RFC 7845 (OutputGainQ8, header validations).
> - **Repacketizer & Packet Padding**: Merging and splitting of Opus frames without decoding/re-encoding, along with CBR packet padding for single-stream and multistream audio.
> - **Soft Clipping**: Non-linear dynamic range compression for float32 PCM (`SoftClip` and `SoftClipper`) to prevent harsh clipping distortion.
> - **Multichannel Surround**: Support for 5.1 and 7.1 surround sound audio playback and decoding.
> - **Concurrency Safety**: Full mutex synchronization across `opus.Decoder`, `opus.Encoder`, `opus.Repacketizer`, and `player.OpusPlayer`.
> - **Memory Safety**: Heap staging buffers and chunked block allocator in `libcshim` to eliminate pointer instability.
> - **Performance**: Slice-by-8 parallel CRC32, Ogg multi-packet page batching, and byte resynchronization.
> - **Float32 & Int16**: First-class support for both normalized float32 `[-1.0, 1.0]` and 16-bit linear PCM.
> - **Compile-Time Guard**: Rejection of 32-bit builds to prevent runtime pointer misalignment.

> [!WARNING]
> **Notice & Usage Disclaimer for Third Parties**
>
> If you plan to use this library in your own projects, please be aware of the following:
> 1. **Personal Project Focus**: This library is maintained primarily for personal project needs. APIs and internal behavior may evolve based on author requirements.
> 2. **Not a Drop-in Replacement**: Due to extensive modifications, refactoring, and module rename, this library is not guaranteed to remain API-compatible with upstream `kazzmir/opus-go`.
> 3. **Architecture Constraint**: Only **64-bit little-endian architectures** (`amd64`, `arm64`) are supported. 32-bit systems (`386`, `arm`, `wasm`) are intentionally blocked at compile time.
> 4. **License & As-Is**: Provided under the original 2-Clause BSD License without warranties of any kind. Use at your own risk.

## Installation

```sh
go get github.com/selawe/go-opus-codec
```

## Directory Structure

- `cmd/oggopusdump`: command-line tool to dump Ogg/Opus headers and packet sizes
- `cmd/oggopusextract`: command-line tool to extract Opus audio packets
- `cmd/oggopus2wav`: command-line tool to decode Ogg/Opus
- `cmd/wav2oggopus`: command-line tool to encode WAV to Ogg/Opus
- `ogg` - RFC 3533 Ogg bitstream and RFC 7845 Ogg Opus container demuxer/muxer
- `opus` - RFC 6716 / RFC 8251 encoder and decoder API with packet inspection and PLC
- `player` - High-level streaming player with frame-accurate seeking
- `opuscc` - Transpiled libopus C source of the decoder logic
- `opusccenc` - Transpiled libopus C source of the encoder logic
- `libcshim` - Small libc shim for the transpiled C code
- `wav` - 16-bit linear PCM WAV parser and writer
- `examples` - Example programs using the library

## Supported Platforms & Architecture

`go-opus-codec` is 100% pure Go with no C compiler, headers, or cgo needed (`CGO_ENABLED=0` friendly). It can be cross-compiled cleanly across all major operating systems.

| Operating System | Architecture | Status | Notes |
|---|---|:---:|---|
| **Linux** | `amd64`, `arm64` | ✅ Supported | Full support (x86-64, AWS Graviton, Raspberry Pi 4/5) |
| **macOS (Darwin)** | `arm64`, `amd64` | ✅ Supported | Full support (Apple Silicon M1-M4 and Intel Macs) |
| **Windows** | `amd64`, `arm64` | ✅ Supported | Full support (Windows 64-bit and Windows on ARM) |
| **FreeBSD** | `amd64` | ✅ Supported | Full support |

> [!NOTE]
> **Architecture Requirement**: The underlying transpiled libopus code targets 64-bit little-endian systems (LP64). Attempting to build for 32-bit architectures (such as `386` or `arm`) is guarded against at compile time to prevent runtime pointer misalignment.

## Minimal high level API example
Decoding an opus file to get PCM samples. Note the sample rate of the PCM data is always 48000 Hz.
```go
package main

import (
    "io"

    opusgo "github.com/selawe/go-opus-codec"
)

func main() {
    player, _ := opusgo.NewPlayerFromFile("file.opus", true) // true means stream from disk
    defer player.Close()

    data := make([]byte, 48000*2*2) // 1 second buffer for stereo s16le
    io.ReadFull(player, data)
    // data now contains PCM samples
}
```

## Minimal low level API example

Decoding an opus file to get PCM samples. Note the sample rate of the PCM data is always 48000 Hz.
```go
package main

import (
    "os"

    "github.com/selawe/go-opus-codec/ogg"
    "github.com/selawe/go-opus-codec/opus"
)

func main() {
    // error handling omitted for brevity
    input, _ := os.Open("file.opus")
    defer input.Close()

    reader, _ := ogg.NewOpusReader(input) // input can be any io.Reader
    decoder, _ := opus.NewDecoderFromHead(reader.Head)
    defer decoder.Close()

    for {
        packet, err := reader.ReadAudioPacket()
        if err != nil {
            // no more audio packets
            break
        }
        decoded, n, _ := decoder.DecodePacket(packet, nil) // nil can instead be a pre-allocated []int16
        // decoded is an []int16 PCM audio buffer with n samples per channel
        _ = decoded
        _ = n
    }
}
```

## Example program usage

From this folder:

- Dump headers + packet sizes:

```sh
go run ./cmd/oggopusdump --no-crc path/to/file.ogg
```

- Extract length-prefixed audio packets:

```sh
go run ./cmd/oggopusextract --out packets.bin path/to/file.ogg
```

`packets.bin` format: repeating `(u32le length, length bytes packet)`.

- Decode `.opus` to `.wav` (48kHz s16le):

```sh
go run ./cmd/oggopus2wav --out out.wav path/to/file.opus
```

- Profile decoding:

```sh
go run ./cmd/oggopus2wav --cpuprofile cpu.pprof --out out.wav path/to/file.opus
go tool pprof -top cpu.pprof
```

- Encoding `.wav` to `.opus`:

```sh
go run ./cmd/wav2oggopus --bitrate 64000 --out out.opus path/to/file.wav
```
