# Ogg Opus Stream Mux & Demux Example

Demonstrates how to create an RFC 7845 compliant Ogg Opus stream from scratch in pure Go without external tools, and read it back using `ogg.PacketWriter` and `ogg.OpusReader`.

## What This Example Demonstrates
1. **Container Muxing (`ogg.PacketWriter`)**:
   - Writing the mandatory `OpusHead` identification header (channel count, pre-skip from encoder lookahead, input sample rate).
   - Writing the `OpusTags` comment header (vendor string and user comments like `TITLE`, `ARTIST`, `GENRE`).
   - Calculating granule positions per RFC 7845 (`PreSkip + totalSamples`) and marking End-Of-Stream (EOS).
2. **Container Demuxing (`ogg.OpusReader`)**:
   - Parsing and validating `OpusHead` and `OpusTags`.
   - Extracting comment tags using case-insensitive lookup (`reader.Tags.Get("TITLE")`).
   - Iterating audio packets with `reader.ReadAudioPacket()`.
3. **Audio Decoding**:
   - Feeding raw audio packets from the Ogg container into `opus.Decoder` to produce decoded PCM samples.

## How to Run

```bash
cd examples/stream_mux
go run .
```

## Expected Output

```text
================================================================
 Ogg Opus Stream Mux & Demux Example (Pure Go Container)
================================================================
Demonstrates:
 1. Muxing an RFC 7845 / RFC 3533 Ogg Opus stream in pure Go
 2. Generating OpusHead, PreSkip lookahead, and OpusTags metadata
 3. Demuxing and inspecting container headers with ogg.OpusReader
 4. Decoding stream packets back to PCM samples

--- Step 1: Muxing Ogg Opus Stream ---
 [Muxer] Wrote OpusHead: channels=2, preSkip=312, inputRate=48000
 [Muxer] Wrote OpusTags: 4 comments, vendor="go-opus-codec"
 [Muxer] Successfully wrote 10 audio frames into Ogg stream (2281 bytes total)

--- Step 2: Demuxing & Reading Stream ---
 [Reader] Parsed OpusHead:
          Version        : 1
          Channels       : 2
          PreSkip        : 312 samples
          Input Rate     : 48000 Hz
          Mapping Family : 0
 [Reader] Parsed OpusTags:
          Vendor         : go-opus-codec
          Title          : Synthetic Tone Stream
          Artist         : go-opus-codec Example
          Genre          : Audio Test
          Encoder        : github.com/selawe/go-opus-codec

--- Step 3: Decoding Demuxed Audio Packets ---
 [Reader] Packet  1: 266 bytes, Granule:  1272, Decoded: 960 samples (EOS: false)
 [Reader] Packet  2: 187 bytes, Granule:  2232, Decoded: 960 samples (EOS: false)
 [Reader] Packet  3: 181 bytes, Granule:  3192, Decoded: 960 samples (EOS: false)
 [Reader] Packet  4: 170 bytes, Granule:  4152, Decoded: 960 samples (EOS: false)
 [Reader] Packet  5: 161 bytes, Granule:  5112, Decoded: 960 samples (EOS: false)
 [Reader] Packet  6: 161 bytes, Granule:  6072, Decoded: 960 samples (EOS: false)
 [Reader] Packet  7: 161 bytes, Granule:  7032, Decoded: 960 samples (EOS: false)
 [Reader] Packet  8: 161 bytes, Granule:  7992, Decoded: 960 samples (EOS: false)
 [Reader] Packet  9: 161 bytes, Granule:  8952, Decoded: 960 samples (EOS: false)
 [Reader] Packet 10: 161 bytes, Granule:  9912, Decoded: 960 samples (EOS: true)

 [Summary] Total Packets Read  : 10
           Total Samples Decoded: 9600 per channel (200.00 ms @ 48000 Hz)
================================================================
 Ogg Opus Stream Mux & Demux completed successfully.
================================================================
```
