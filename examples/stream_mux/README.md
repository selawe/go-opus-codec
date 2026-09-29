# Ogg Opus Stream Mux & Demux Example

Demonstrates how to create an RFC 7845 compliant Ogg Opus stream from scratch in pure Go without external tools, and read it back using `ogg.PacketWriter` and `ogg.OpusReader`.

## What This Example Demonstrates
1. **Container Muxing (`ogg.PacketWriter`)**:
   - Writing the mandatory `OpusHead` identification header (channel count, pre-skip from encoder lookahead, input sample rate).
   - Writing the `OpusTags` comment header (vendor string and user comments like `TITLE`, `ARTIST`, `GENRE`).
   - Calculating granule positions per RFC 7845 Section 4: interior pages carry the number of samples decoded so far (pre-skip included), and only the final page is trimmed to `PreSkip + input length` and marked End-Of-Stream (EOS).
   - Encoding extra frames after the input ends so the encoder's lookahead (the pre-skip) is flushed and no input sample is lost.
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
 [Muxer] Successfully wrote 11 audio frames into Ogg stream (2470 bytes total)

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
 [Reader] Packet  1: 266 bytes, Granule:   960, Decoded: 960 samples (EOS: false)
 [Reader] Packet  2: 187 bytes, Granule:  1920, Decoded: 960 samples (EOS: false)
 [Reader] Packet  3: 181 bytes, Granule:  2880, Decoded: 960 samples (EOS: false)
 [Reader] Packet  4: 170 bytes, Granule:  3840, Decoded: 960 samples (EOS: false)
 [Reader] Packet  5: 161 bytes, Granule:  4800, Decoded: 960 samples (EOS: false)
 [Reader] Packet  6: 161 bytes, Granule:  5760, Decoded: 960 samples (EOS: false)
 [Reader] Packet  7: 161 bytes, Granule:  6720, Decoded: 960 samples (EOS: false)
 [Reader] Packet  8: 161 bytes, Granule:  7680, Decoded: 960 samples (EOS: false)
 [Reader] Packet  9: 161 bytes, Granule:  8640, Decoded: 960 samples (EOS: false)
 [Reader] Packet 10: 161 bytes, Granule:  9600, Decoded: 960 samples (EOS: false)
 [Reader] Packet 11: 161 bytes, Granule:  9912, Decoded: 960 samples (EOS: true)

 [Summary] Total Packets Read  : 11
           Total Samples Decoded: 10560 per channel (220.00 ms @ 48000 Hz)
           Audible after trimming: 9600 per channel (pre-skip 312 dropped at the start, the rest at the end-of-stream granule)
================================================================
 Ogg Opus Stream Mux & Demux completed successfully.
================================================================
```
