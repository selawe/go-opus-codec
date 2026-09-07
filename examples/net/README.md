# Network Opus Stream Player Example

Demonstrates real-time playback of an HTTP/HTTPS remote Ogg Opus audio stream using `opusgo.NewPlayerFromReader` and `ebitengine/oto/v3`.

## What This Example Demonstrates
1. **Network Streaming**: Decodes incoming HTTP Ogg Opus chunked data on-the-fly without downloading the entire file to disk first.
2. **Context Cancellation & Graceful Shutdown**: Listens for OS interrupts (`SIGINT`, `SIGTERM`) and closes the HTTP response body cleanly.
3. **Sound Card Output**: Uses Oto v3 for cross-platform hardware audio playback (48 kHz, stereo, 16-bit signed integer LE).

## Requirements
On Linux, `ebitengine/oto/v3` requires ALSA development headers:
```bash
sudo apt install libasound2-dev pkg-config
```
*(On macOS and Windows, no extra native packages are required).*

## How to Run

```bash
cd examples/net
go run . "https://example.com/stream.opus"
```
