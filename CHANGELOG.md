# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.2.6] - 2026-09-24

### Fixed
- **SILK/VoIP audio was loud noise in both encoder and decoder** (present since the first release). `libcshim`'s `Uint32FromInt16`, `Uint32FromInt8`, `Uint64FromInt16`, `Uint64FromInt32` and `UintptrFromInt32` zero-extended negative values instead of sign-extending them as C does, and the same casts were inlined into the transpiled SILK decoder. The decoder output saturated to full scale on SILK and hybrid packets, and the encoder produced garbled audio at several times the target bitrate in `ApplicationVoIP` at speech bitrates, which is the typical WebRTC voice setup. CELT-only streams (e.g. `ApplicationAudio` at higher bitrates) were not affected. Decoder output now matches reference libopus within ±1 LSB on all 12 RFC 6716 test vectors, and the encoder's output matches libopus (`64abaca`)

### Added / Test
- `TestRFC6716_PCMOutput`: compare decoded PCM against the reference `.dec` files. The existing conformance matrix only checks the range decoder's final state and could not detect synthesis errors (`64abaca`)
- `TestSILKRoundtrip`: VoIP encode/decode roundtrip at 12/16/32 kbps checking bitrate and SNR (`64abaca`)
- Sign-extension tests for the `libcshim` integer conversion helpers (`64abaca`)

## [0.2.5] - 2026-09-23

### Fixed
- `ogg.PacketReader`: Replaced the 64k allocation FIXME with a deterministic, spec-compliant max-page-size guarantee, preventing potential truncation panics or misses on malformed edge-cases.
- `libcshim`: Added comprehensive architectural documentation for internal `abort`, `assert`, and panic patterns. Verified and documented that `TLS.Free` panics cannot be weaponized via external stream bytes.
- Resolved over 150 unchecked errors (`errcheck`), redundant type conversions (`unconvert`), and formatting misses across core components and test files.

### CI & Tooling
- **golangci-lint upgrade:** Migrated the linting framework from v1 to v2 (v2.13.2) across `.golangci.yml` and GitHub Actions. Integrated `errorlint`, `unconvert`, `misspell`, `gofmt`, dan `revive` untuk kualitas kode jangka panjang.
- Elevated unit test code coverage on `player` and `wav` packages (>76%+) by explicitly exercising fault-injection logic (RIFF misalignments, EOF handling).

## [0.2.4] - 2026-09-22

### Fixed
- `opus.NewMultistreamEncoder`: no longer panics with a `TLS.Free underflow` when the underlying C call rejects the parameters (e.g. an unsupported sample rate) (`996e308`)
- `opus.NewEncoderFromHead`: validate `InputSampleRate` against the Opus-supported rates instead of only checking for zero, so headers with e.g. 44100 Hz fall back to 48000 correctly (`996e308`)
- `opus.Repacketizer.Out`/`OutRange` and `opus.SoftClip`/`SoftClipper.Process`: stage caller-supplied slices into a heap-backed buffer before passing raw pointers into transpiled C code, matching `Decoder`/`Encoder` (`996e308`)
- `opus.Decoder.DecodePacket`/`DecodePacketF32`: Packet Loss Concealment now synthesizes the stream's actual last frame duration instead of always 120ms (`996e308`)
- `ogg.PageReader.ReadPage`: a truncated final page now returns a clean `io.EOF` instead of `ErrResyncFailed` (`c1ca5d1`)
- `ogg.PacketReader.SeekToPage`/`LastPageGranule`: preserve `VerifyCRC`/`Resync`/`MaxResync` across seeks, treat an invalid granule position correctly, and handle seeking into a continued page without failing (`c1ca5d1`)
- `wav.Writer.WriteInt16PCM`: reject writes that would overflow the RIFF 4 GiB size limit instead of silently wrapping the byte counter (`7e3f7ed`)
- `wav.Reader`: truncated data chunks return the partial samples read instead of an error; accept `WAVE_FORMAT_EXTENSIBLE` PCM; reject a zero sample rate (`7e3f7ed`)
- `player.OpusPlayer`: no longer replays stale PCM after a failed decode, no longer returns a premature `(0, nil)` before real EOF, corrects pre-skip trim accounting on the first packet, rejects negative seek offsets, and resets the finished flag on seek (`4c4ab32`)
- `EncodeWAVToOggOpus`/`wav2oggopus`: always flush the encoder lookahead and emit a final EOS page, including for empty input; `ConvertWAVFileToOggOpus`/`ConvertOggOpusFileToWAV` remove partially written output on failure (`67b73ba`)
- `libcshim.Xmalloc`: return 0 on an oversized allocation instead of panicking; `Xfprintf` now formats its C varargs instead of ignoring them (`5a620a5`)

### Added
- `opus.Decoder.DecodePacketFEC`/`DecodePacketFECF32`: decode libopus in-band FEC data from the next packet to recover a lost frame (`996e308`)
- `opus.Decoder.LastFrameSize`/`SetLastFrameSize` to inspect/hint the PLC concealment frame size (`996e308`)
- `DecodeOggOpusToWAV`/`ConvertOggOpusFileToWAV`: optional `WithMaxOutputBytes` to cap decoded PCM output size (`67b73ba`)

### CI & Tooling
- Release workflow now runs the full RFC 6716 conformance suite and fails (instead of silently skipping) if test vectors are missing (`83f2318`)
- Add a Go version matrix (1.24 and stable), a `go vet -unsafeptr=false` step over hand-written packages, a check that `GOARCH=386` fails to build, and a coverage report upload (`83f2318`)
- Add a daily scheduled fuzzing workflow across all fuzz targets, including new `ogg.PageReader`, `ogg.PacketReader`, `wav.NewReader`, and `opus.Repacketizer` targets (`83f2318`)

## [0.2.3] - 2026-09-11

### Performance
- Eliminate heap escapes in `VaList` achieving `0 B/op` and `0 allocs/op` in hot-path steady-state decode and encode (`986e1cf`)
- Pool stateless TLS instances in `PacketPad`, `PacketPadSafe`, and `SoftClip` via `sync.Pool` (`30b4681`)
- Optimize TLS implementation by removing unnecessary mutexes for heap and keys (`8685e0e`)

### Added / Test
- Add fuzz testing suite for Opus packet handling and decoding (`FuzzPacketFrames`, `FuzzPacketUnpad`, `FuzzDecode`, `FuzzDecodeF32`) (`300789f`)
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
