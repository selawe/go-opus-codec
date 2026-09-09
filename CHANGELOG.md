# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

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
