# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.3.1] - 2026-09-29

Patch release: fixes an output-handling regression introduced in 0.3.0 and makes the test suite portable to Windows and macOS. No API changes.

### Fixed
- `internal/atomicfile` (used by `wav2oggopus`, `oggopus2wav`, `oggopusextract` and the `Convert*File` helpers) opened device destinations with `O_TRUNC`, which Windows rejects for `NUL` (`open NUL: Incorrect function`), so `-out NUL` failed there. Only regular files are truncated now (`0448613`)
- Symlink and device destinations are written in place. Previously a symlink such as `/dev/stdout` was renamed over, so `wav2oggopus -out /dev/stdout > file` failed or lost its data when stdout was redirected to a file (`0448613`)

### Tests
- The permission-bit assertion is skipped on Windows, and the `GODEBUG=efence` stack-residency test runs only on Linux, so the Windows and macOS CI jobs pass (`ed05463`)

### CI & Tooling
- The GitHub Release body is now built from the matching `CHANGELOG.md` section by `scripts/release_notes.sh`, followed by install and verification notes and GitHub's generated list. The release fails if the changelog has no section for the tag (`1ea7b10`)
- `.gitattributes` forces LF for `*.sh`, so shell scripts still run in CRLF checkouts (`d7a37b4`)

## [0.3.0] - 2026-09-29

Minor release (pre-1.0 semver): it adds public API (`ogg.ErrTruncatedPage`, `Discontinuity`, `PacketWriter.MaxPagePackets`, `ogg.MaxOpusHeaderPacketSize`) and changes behavior in a few places. Files written by earlier versions are unaffected and still play; the granule fix only changes what new files contain.

Behavior changes to be aware of when upgrading:
- Encoded files use RFC 7845 granule positions (no pre-skip offset on interior pages) and about-one-second pages.
- `ogg.PacketReader` drops packets damaged by a page-sequence gap instead of returning them corrupt.
- A file cut inside a page now returns `ogg.ErrTruncatedPage` instead of a plain `io.EOF`. It still matches `errors.Is(err, io.EOF)`, but code comparing with `err == io.EOF` sees only clean ends.
- The file conversion helpers and CLI tools refuse to write onto their input and only replace an existing output after success.

### Fixed
- **Ogg granule positions were off by the pre-skip on every page but the last** in `EncodeWAVToOggOpus`/`ConvertWAVFileToOggOpus`, `wav2oggopus`, `examples/convert` and `examples/stream_mux`. Interior pages carried `PreSkip + encoded samples` instead of `encoded samples` (RFC 7845 Section 4), so the sequence could decrease before the EOS page and libopus-based decoders such as ffmpeg dropped the last pre-skip (312) samples. Only the EOS page carries `PreSkip + input length` now (`3321d4b`, `b0868bc`, `05b80b0`, `ba87aca`)
- `opus.Decoder.Decode`/`DecodeF32` passed the caller's packet to the transpiled code as a raw `uintptr` without staging it in heap memory. A stack-resident packet goes stale when the goroutine stack grows and faults in the range decoder under `GODEBUG=efence=1`. The packet is now copied into a decoder-owned buffer, with no measurable slowdown (`efc3242`). The multistream `mapping` table is copied to the heap too, as a precaution (`d54099a`)
- `ogg.PacketReader` never checked `PageSequence`: after a lost or corrupt page the next continuation page was appended to the stale bytes and returned as one corrupt packet with no error. A gap now drops the damaged packet, skips the orphaned continuation and sets `Discontinuity` on the next packet (`f8f81d4`)
- `ogg.NewOpusReader` capped every packet, headers included, at 64 KiB, so files with embedded cover art (a large `METADATA_BLOCK_PICTURE` in OpusTags) could not be opened. Headers may now be up to `ogg.MaxOpusHeaderPacketSize` (16 MiB); audio packets keep the 64 KiB cap (`cad8dbe`)
- `Length()`, `TotalSamples()` and `TotalDuration()` on `player.OpusPlayer` and `ogg.OpusReader` ended playback when called mid-stream on a seekable source. `PacketReader.LastPageGranule` now scans with a private page reader and restores the position (`32effd4`)
- `player.OpusPlayer.Read` panicked (`slice bounds out of range`) on stereo streams whose final granule is below the pre-skip (`6cd1592`)
- Seeking in `player.OpusPlayer` started a cold decoder exactly at the target page, so the first audio after `SeekSample`/`SeekTime` was wrong (mean error of about 800-1300 LSB on the bundled test file). It now decodes 80 ms of pre-roll as RFC 7845 Section 4.6 requires (`3cc9007`)
- `ConvertWAVFileToOggOpus`, `ConvertOggOpusFileToWAV`, `wav2oggopus` and `oggopusextract` could destroy data: an output path equal to the input truncated or overwrote it (`wav2oggopus` even exited 0), a failed run deleted a pre-existing output, and `Flush`/`Close` errors were ignored so a full disk still succeeded. Output now goes through `internal/atomicfile`: the input can not be overwritten, an existing destination is replaced only after success, and failures leave no partial file (`78ca55b`, `fff8bc9`, `3ec4757`, `01413a9`)
- `oggopus2wav` called `os.Exit` on errors, skipping `pprof.StopCPUProfile` and leaving an empty CPU profile (`1c8712b`)
- `examples/convert` dropped the last pre-skip samples (no frames were encoded after the input ended) and produced no EOS page for empty input (`d3a2cd6`)

### Added
- `ogg.ErrTruncatedPage`: a stream cut inside a page is no longer indistinguishable from a clean end. It matches `io.ErrUnexpectedEOF`, and still matches `io.EOF` so loops that only stop on `errors.Is(err, io.EOF)` keep working. A clean end remains a plain `io.EOF` (`8c635eb`)
- `ogg.Packet.Discontinuity` and `ogg.OpusAudioPacket.Discontinuity`: set on the first packet after a page-sequence gap, so callers can conceal the lost audio (`f8f81d4`, `c6b6e02`)
- `ogg.PacketWriter.MaxPagePackets` to bound the duration of batched pages (`d2caaeb`)
- `ogg.MaxOpusHeaderPacketSize` (`cad8dbe`)

### Changed
- `EncodeWAVToOggOpus`, `wav2oggopus` and `examples/convert` batch audio packets into Ogg pages of about one second (max 8 KiB) instead of one page per packet, and put OpusTags on its own page as RFC 7845 requires. A 64 kbps stereo file is about 14% smaller; the saving is much larger at low bitrates (`d96b42c`, `8dae4c5`, `2a9f2d9`)
- `player.OpusPlayer.Seek(0, io.SeekCurrent)` answers the position without re-seeking or rebuilding the decoder (`e85fc03`)
- A damaged packet after a page-sequence gap is now dropped instead of being returned corrupt. Intact streams are unaffected
- `player`/`ogg` documentation: `Length`, `TotalSamples` and `TotalDuration` are non-destructive on seekable input and consume the stream otherwise

### Docs
- README: documented that `go test -race` needs `-gcflags=all=-d=checkptr=0` (the transpiled code addresses Go memory through `uintptr`, which `checkptr` aborts on), the race test package list, output safety, granule/page-batching/damaged-input guidance, and refreshed the benchmark table. An A/B run against 0.2.7 showed no codec performance change; the table differences come from re-measuring on the same CPU under WSL2 (`fb81cd3`, `ddc88d8`, `01e465e`)

## [0.2.7] - 2026-09-24

No library code changes since 0.2.6. This release exists because the CI and release workflows had been failing since 0.2.4, so no GitHub Release was published for 0.2.4–0.2.6.

### CI & Tooling
- Unit test jobs no longer fail when the RFC 6716 vectors are absent. The vector tests skip unless `OPUS_REQUIRE_TESTVECTORS` is set, and `scripts/run_conformance.sh` sets it, so the conformance job and the release gate still fail on a missing corpus (`5f8cc8d`)
- `scripts/run_conformance.sh` also runs `TestRFC6716_PCMOutput` (`5f8cc8d`)
- Examples `convert`, `decode`, `net` and `play` now declare `go 1.24.0`, matching the Go version CI installs (`5f8cc8d`)
- The `go vet` step runs under bash on Windows runners (`5f8cc8d`)
- The examples job installs `libasound2-dev`, which `examples/net` and `examples/play` need through oto's ALSA backend (`90fa0a5`)

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
