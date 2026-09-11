# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.2.3] - 2026-09-11

### Performance
- Eliminate heap escapes in `VaList` achieving `0 B/op` and `0 allocs/op` in hot-path steady-state decode and encode (`986e1cf`)
- Pool stateless TLS instances in `PacketPad`, `PacketPadSafe`, and `SoftClip` via `sync.Pool` (`30b4681`)
- Optimize TLS implementation by removing unnecessary mutexes for heap and keys (`8685e0e`)

### Added / Test
- Add fuzz testing suite for Opus packet handling and decoding (`FuzzPacketPad`, `FuzzPacketUnpad`, `FuzzDecode`, `FuzzDecodeF32`) (`300789f`)
- Add tests for player with large audio packets exceeding typical frame sizes (`b184005`)
- Add comprehensive benchmark suites for encoder and decoder throughput (`c3ce4ee`)
- Add Phase 1 real-world configuration matrix benchmarks (2.5ms–60ms, 16kHz–48kHz, mono/stereo, complexities 1–10) (`0e3ceff`, `876ef3e`)
- Add steady-state page reader and parallel codec benchmarks (`eecdb55`)
- Add Phase 2 realtime network benchmarks for PLC, Repacketizer, and Padding (`14be38e`)
- Add decoder/encoder warmup, FEC, and SoftClip benchmarks (`4087262`)
- Expand benchmark corpus to all 12 RFC 6716 test vectors in codec comparison (`876ef3e`)

## [0.2.2] - 2026-09-09

### Added / Test
- Comprehensive unit tests and testable CLI harness for `cmd/oggopusdump`, `cmd/oggopusextract`, and `cmd/wav2oggopus` (`f8f2302`)
- Detailed failure reporting and refactored vector runner in RFC 6716 conformance matrix test (`e41c0f4`)
- Unhandled error checks in player seek tests and copy benchmarks (`0c7b578`)

### CI & Tooling
- GitHub Actions RFC 6716 conformance matrix job with caching (`d2c71e1`)
- `make lint` target and golangci-lint CI step (`d2c71e1`)

### Fixed / Refactor
- Handle unhandled return values from `Discard` and `Seek` in `ogg` package; remove dead CRC helper (`b238d6d`)
- Remove redundant nil check in `opus.Decoder` and clean up test helper code (`4f5fe95`)
- Remove write-only `haveData` variable in `wav.Reader` (`f30f60b`)

## [0.2.1] - 2026-09-09

### Security / Fixed
- Prevent use-after-free in `Repacketizer.Cat` with retained buffer copies (`cc88142`)
- Validate parameters and mapping length in `NewMultistreamDecoder` to prevent an out-of-bounds heap read (`86f9cab`)
- Free TLS pseudostack in `Repacketizer.Close` and its create error path (`f2dca6e`)
- Validate packet loss percentage range in `SetPacketLossPerc` instead of silently clamping (`61e01d9`)
- Enforce RFC 7845 `OpusHead` version and channel mapping family validation (`3d6182b`)
- Enforce maximum packet size limits to prevent unbounded memory allocation (`22dfdce`)
- Clamp `TotalSamples` to zero on truncated streams where granule position is less than pre-skip (`7e07e6c`)
- Enforce RFC 6716 `MaxFrameSize` (1275 bytes) limit across all packet codes; document stream concurrency requirements for `ogg`/`wav` packages (`088fb60`)

### Documentation
- Clarify standard production vs internal compliance application constants (`e0bea84`)
- Align README with `go.mod` (Go 1.24+) and the actual `LICENSE` file (BSD 3-Clause) (`564dd8c`)

## [0.2.0] - 2026-09-08

### Added
- High-level WAV and Ogg Opus conversion helpers (`0299a10`)
- Multistream Encoder and `NewEncoderFromHead` for multichannel audio (`c4447c4`)
- `SetVolume`, `Volume`, `SetGain`, and `Gain` controls to `OpusPlayer` (`cc4449f`)

### Performance
- Reuse lacing buffer and page header in `PacketWriter` to achieve zero-alloc streaming (`69145a3`)

### CI
- Add developer Makefile, golangci-lint config, and comprehensive multi-OS cross-compile GitHub workflow (`8a839f8`)

## [0.1.1] - 2026-09-08

### Fixed
- Prevent infinite loop on unaligned player reads and ensure frame alignment (`4067b8e`)
- Guard against unbounded WAV `fmt` chunk allocation and handle odd-sized chunks (`92441d5`)
- Fix player/libcshim timestamp subsecond precision and guard null base pointers (`22868da`)
- Add `KeepAlive` protections, early padding validation, and pseudostack TLS cleanup in the `opus` package (`d358285`)

### Performance
- Reuse internal byte buffer in `ReadInt16PCM` and validate CLI channel arguments (`a82ba3e`)

## [0.1.0] - 2026-09-07

Initial public release: pure Go Ogg/Opus parser, decoder, encoder, WAV I/O, and player, transpiled from libopus 1.6.1 via ccgo with a hand-written Go wrapper API.
