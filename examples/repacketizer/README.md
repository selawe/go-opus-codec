# Opus Repacketizer Example

Demonstrates how to use `opus.Repacketizer` to merge multiple small Opus packets into a single multi-frame packet (or split/extract sub-ranges) without decoding and re-encoding.

## What This Example Demonstrates
1. **Zero-Loss Packet Bundling**: Merging three 20ms packets into a single 60ms Opus packet (`rp.Cat()` and `rp.Out()`). In VoIP, WebRTC, and RTP streaming, this drastically reduces IP/UDP/RTP header overhead (often 40-60 bytes per packet) over the network.
2. **Packet Inspection**: Inspecting frame counts (`opus.PacketFrames()`), samples per frame (`opus.PacketSamplesPerFrame()`), and total packet duration (`opus.PacketSamples()`).
3. **Sub-Range Frame Extraction**: Extracting a subset of frames with `rp.OutRange(begin, end, out)`.
4. **Seamless Decoding**: Decoding the merged 60ms packet in a single call to `dec.Decode()` yielding 2880 samples per channel.

## How to Run

```bash
cd examples/repacketizer
go run .
```

## Expected Output

```text
================================================================
 Opus Repacketizer Example (RTP / WebRTC Frame Bundling)
================================================================
Demonstrates merging multiple small Opus packets into a single
multi-frame packet (and extracting sub-ranges) without re-encoding.

 [Encoder] Generated 20ms Frame 1: 266 bytes (TOC: 0xfc)
 [Encoder] Generated 20ms Frame 2: 187 bytes (TOC: 0xfc)
 [Encoder] Generated 20ms Frame 3: 181 bytes (TOC: 0xfc)

 [Repacketizer] Buffered frames count: 3
 [Repacketizer] Merged Packet Size : 636 bytes
 [Repacketizer] Frames in Packet   : 3
 [Repacketizer] Samples per Frame  : 960 (20 ms)
 [Repacketizer] Total Audio Duration: 2880 samples (60 ms)

 [Repacketizer] OutRange(1, 3) Extracted: 368 bytes (2 frames)

 [Decoder] Successfully decoded merged packet!
           Decoded: 2880 samples per channel (5760 total PCM samples)
================================================================
 Repacketizer demonstration completed successfully.
================================================================
```
