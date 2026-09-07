# Opus Roundtrip PCM Example

Demonstrates the low-level encoding and decoding pipeline using pure Go `opus.Encoder` and `opus.Decoder`.

## What This Example Demonstrates
1. **Dynamic Range Compression (`opus.SoftClip`)**: Smoothly compresses `float32` audio peaks exceeding `[-1.0, 1.0]` without harsh digital clipping before encoding.
2. **Encoder Tuning**: Configuring bitrate, Variable Bitrate (VBR), encoding complexity (1..10), expected packet loss percentage, and in-band Forward Error Correction (FEC).
3. **Entropy Range Verification (`FinalRange`)**: Introspecting the entropy coder state to guarantee bit-exact alignment between encoder and decoder.
4. **Packet Loss Concealment (PLC)**: Simulating network packet loss by passing `nil` to `dec.Decode(nil, pcm, frameSize, false)`. The decoder reconstructs audio smoothly without clicks, pops, or panics.
5. **Compression Ratio**: Measuring raw PCM size versus compressed Opus packet size (typically $> 90\%$ bandwidth reduction).

## How to Run

```bash
cd examples/roundtrip_pcm
go run .
```

## Expected Output

```text
================================================================
 Opus Roundtrip PCM Example (Encode, Decode, SoftClip, & PLC)
================================================================
Demonstrates:
 1. Float32 SoftClip dynamic range compression on hot audio peaks
 2. Encoder configuration: Bitrate, VBR, Complexity, In-Band FEC
 3. Transmission simulation with intentional packet loss
 4. Decoder Packet Loss Concealment (PLC) on lost frames
 5. Entropy FinalRange inspection and audio quality metrics

--- Step 1: Float32 SoftClip Demo ---
 [SoftClip] Peak amplitude before clipping: 1.800 (exceeds 1.0!)
 [SoftClip] Peak amplitude after SoftClip  : 1.000 (smoothly bounded in [-1.0, 1.0])

--- Step 2: Encoder Configuration & Encoding ---
 [Encoder] Sample Rate : 48000 Hz (2 channels)
 [Encoder] Bitrate     : 64000 bps
 [Encoder] VBR Enabled : true
 [Encoder] Complexity  : 10 / 10
 [Encoder] In-Band FEC : true (20% expected loss)
 [Encoder] Lookahead   : 312 samples

 [Encoder] Frame 1: raw 3840 bytes -> compressed 121 bytes (FEC/LBRR: false, Range: 0x02212500)
 [Encoder] Frame 2: raw 3840 bytes -> compressed 218 bytes (FEC/LBRR: true , Range: 0x48215d00)
 [Encoder] Frame 3: raw 3840 bytes -> compressed 341 bytes (FEC/LBRR: true , Range: 0x0e6d8500)
 [Encoder] Frame 4: raw 3840 bytes -> compressed 317 bytes (FEC/LBRR: true , Range: 0x1c17c100)
 [Encoder] Frame 5: raw 3840 bytes -> compressed 281 bytes (FEC/LBRR: true , Range: 0x02cffa00)
 [Encoder] Total Compression Ratio: 15.02:1 (93.3% space saving)

--- Step 3: Decoding & Packet Loss Concealment (PLC) ---
 [Decoder] Frame 1: decoded 960 samples, SNR:  -0.7 dB (Range: 0x02212500)
 [Decoder] Frame 2: decoded 960 samples, SNR:  -9.0 dB (Range: 0x48215d00)
 [Decoder] Frame 3: [DROPPED / LOSS] -> PLC concealed 960 samples smoothly!
 [Decoder] Frame 4: decoded 960 samples, SNR:  -9.9 dB (Range: 0x1c17c100)
 [Decoder] Frame 5: decoded 960 samples, SNR:  -9.8 dB (Range: 0x02cffa00)

================================================================
 Opus Roundtrip PCM demonstration completed successfully.
================================================================
```
