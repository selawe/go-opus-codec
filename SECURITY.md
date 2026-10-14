# Security policy

## Supported versions

Security fixes go into the latest minor release (and `master`). Older releases are not patched; upgrade instead.

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/selawe/go-opus-codec/security/advisories/new)
("Report a vulnerability" on the repository's Security tab) rather than in a public issue. Include the
version, a minimal input or program that reproduces it, and what you observed (crash, hang, memory or
disk growth, wrong output). You can expect an acknowledgement within a week.

## What counts

The library parses untrusted input (Ogg, Opus packets, WAV headers) and passes it to a transpiled codec, so
the interesting classes are:

- a panic, hang or crash reachable from crafted input through the public API or the command line tools,
- unbounded memory, CPU or disk use out of proportion to the input,
- memory-safety problems in the `uintptr`-based codec glue (`libcshim`, `opus`),
- a command line tool leaving or overwriting files it should not.

Findings in the machine-transpiled `opuscc` / `opusccenc` packages that also exist in upstream libopus 1.6.1
are best reported to [libopus](https://opus-codec.org/) as well.

## Hardening notes for applications

- Decoding to WAV: pass `opusgo.WithMaxOutputBytes(opusgo.DefaultMaxOutputBytes)` for untrusted input. A tiny
  Opus packet can decode to 120 ms of audio, so a small file can expand a thousandfold.
- `ogg.PacketReader` and `ogg.OpusReader` cap packet sizes (`SetMaxPacketSize`); keep the defaults for
  untrusted streams.
- `wav.Reader` rejects sample rates above 768 kHz and `resample.Check` bounds the filter table, so a hostile
  header cannot request an enormous resampler.
- The transpiled codec addresses Go memory through `uintptr`, so `go test -race` needs
  `-gcflags=all=-d=checkptr=0` (see the README).
