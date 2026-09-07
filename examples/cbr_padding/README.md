# Opus CBR Packet Padding & Unpadding Example

Demonstrates how to use `opus.PacketPad` and `opus.PacketUnpad` to enforce Constant Bitrate (CBR) uniform packet sizes over the wire.

## What This Example Demonstrates
1. **Traffic-Analysis Resistance**: In secure VoIP (SRTP, WireGuard, VPNs), variable-size Opus packets leak speech cadence and phoneme structures. Padding every packet to a uniform size (e.g. 500 bytes) prevents eavesdroppers from inferring conversation activity.
2. **RFC 6716 Compliant Padding (`opus.PacketPad`)**: Extends an Opus packet to any target length by formatting RFC 6716 padding bytes.
3. **Transparent Decoding**: Demonstrates that standard Opus decoders automatically parse and ignore padding bytes without any external configuration.
4. **Zero-Fidelity Loss Stripping (`opus.PacketUnpad`)**: Strips padding bytes and verifies that the resulting payload is bit-for-bit identical to the unpadded original packet.

## How to Run

```bash
cd examples/cbr_padding
go run .
```

## Expected Output

```text
================================================================
 Opus CBR Packet Padding & Unpadding Example
================================================================
Demonstrates:
 1. Padding variable-length Opus packets to a fixed size (CBR)
 2. Preventing packet-size traffic analysis in secure VoIP/VPN
 3. Stripping padding via opus.PacketUnpad
 4. Decoding both padded and unpadded packets with bit-exact fidelity

 [Encoder] Frame 1: variable length = 395 bytes (TOC: 0xfc)
 [Encoder] Frame 2: variable length = 335 bytes (TOC: 0xfc)
 [Encoder] Frame 3: variable length = 263 bytes (TOC: 0xfc)

--- Step 2: Applying opus.PacketPad (Uniform Target: 500 bytes) ---
 [Padder] Frame 1: 395 bytes -> padded to exactly 500 bytes (overhead: +105 bytes)
 [Padder] Frame 2: 335 bytes -> padded to exactly 500 bytes (overhead: +165 bytes)
 [Padder] Frame 3: 263 bytes -> padded to exactly 500 bytes (overhead: +237 bytes)

--- Step 3: Decoding Padded Packets Directly ---
 [Decoder] Direct padded frame 1: decoded 960 samples successfully!
 [Decoder] Direct padded frame 2: decoded 960 samples successfully!
 [Decoder] Direct padded frame 3: decoded 960 samples successfully!

--- Step 4: Stripping Padding with opus.PacketUnpad ---
 [Unpadder] Frame 1: unpadded size = 395 bytes (Matches Original: true)
 [Unpadder] Frame 2: unpadded size = 335 bytes (Matches Original: true)
 [Unpadder] Frame 3: unpadded size = 263 bytes (Matches Original: true)

================================================================
 Opus CBR Packet Padding & Unpadding completed successfully.
================================================================
```
