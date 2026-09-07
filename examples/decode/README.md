# Opus Benchmark Decoder Example

Demonstrates fast decoding of an Ogg Opus (`.opus` or `.ogg`) audio file using the high-level `opusgo.NewPlayerFromFile` API, measuring decoding throughput (kilobytes/second).

## What This Example Demonstrates
1. **High-Level Player API**: Opening an Opus file via `opusgo.NewPlayerFromFile(filename, true)`.
2. **Fast Streaming I/O**: Stream-decoding directly into `io.Discard` via `io.Copy(io.Discard, opusPlayer)`.
3. **Performance Metrics**: Measuring elapsed time and reporting both decoded PCM throughput and input compressed bitstream throughput.

## How to Run

```bash
cd examples/decode
go run . path/to/audio.opus
```
