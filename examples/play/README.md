# Local Opus Audio Player with Seeking

Demonstrates audio playback of local `.opus` or `.ogg` files with sample-accurate seeking and support for both 16-bit linear PCM (`NewPlayerFromFile`) and 32-bit floating point PCM (`NewPlayerF32FromFile`).

## What This Example Demonstrates
1. **Sample-Accurate Seeking**: Using `opusPlayer.Seek(offset, io.SeekStart)` and `opusPlayer.SeekTime(duration)`.
2. **Playback Position Tracking**: Querying `opusPlayer.CurrentSample()` and `opusPlayer.CurrentTime()`.
3. **Float32 & Int16 Output Modes**: Demonstrates both `oto.FormatSignedInt16LE` and `oto.FormatFloat32LE`.
4. **Hardware Audio Output**: Leverages `ebitengine/oto/v3` for low-latency sound playback.

## Requirements
On Linux, `ebitengine/oto/v3` requires ALSA development headers:
```bash
sudo apt install libasound2-dev pkg-config
```
*(On macOS and Windows, no extra native packages are required).*

## How to Run

```bash
cd examples/play
go run . path/to/audio.opus
```
