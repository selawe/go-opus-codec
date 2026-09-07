# MP3 to Ogg Opus Converter Example

Demonstrates how to convert an MP3 audio file into an Ogg Opus container (`.opus`) in Go, using a streaming pipeline with concurrent decoding, linear resampling, and progress reporting.

## What This Example Demonstrates
1. **Streaming Audio Pipeline**: Reads and decodes MP3 using `hajimehoshi/go-mp3` in a background goroutine, feeding decoded PCM bytes through a buffered channel to maximize CPU parallelism.
2. **Linear Resampling**: Resamples arbitrary MP3 input sample rates (e.g. 44.1 kHz) to the standard Opus 48 kHz internal clock.
3. **Opus Encoding**: Configures `opus.Encoder` (64 kbps, VBR, complexity 10) and handles encoder lookahead (`enc.Lookahead()`) for OpusHead `PreSkip`.
4. **Ogg Container Writing**: Packages audio frames into RFC 7845 pages with granule tracking using `ogg.PacketWriter`.
5. **Real-time Progress Bar**: Displays conversion percentage, processed frames, elapsed time, and ETA.

## How to Run

```bash
cd examples/convert
go run . path/to/input.mp3 [path/to/output.opus]
```
