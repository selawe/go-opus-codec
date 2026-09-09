// Package ogg implements reading and writing of Ogg bitstreams (RFC 3533)
// and Ogg Opus audio stream encapsulation (RFC 7845).
//
// Key features:
//   - PageReader: Reads Ogg pages with stream byte resynchronization (RFC 3533 §4).
//   - PacketReader: Reassembles Ogg packets across page boundaries using lacing rules.
//   - PacketWriter: Serializes packets to Ogg pages with multi-packet batching support.
//   - OpusReader: Parses OpusHead identification and OpusTags comment headers.
//   - Slice-by-8 CRC32: Precomputed 8-way parallel tables for high-throughput checksumming.
//
// Concurrency: Types in this package (PageReader, PacketReader, PacketWriter, OpusReader)
// wrap io.Reader or io.Writer and are not safe for concurrent use by multiple goroutines
// without external synchronization.
package ogg

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrInvalidCapturePattern = errors.New("ogg: invalid capture pattern")
	ErrUnsupportedVersion    = errors.New("ogg: unsupported version")
	ErrCRCMismatch           = errors.New("ogg: crc mismatch")
	ErrResyncFailed          = errors.New("ogg: stream resynchronization failed")
)

// Page represents a single Ogg page (RFC 3533).
//
// Note: CRC is verified by ReadPage; if you want to skip verification,
// use PageReader with VerifyCRC=false.
type Page struct {
	Version          uint8
	HeaderType       uint8
	GranulePosition  uint64
	BitstreamSerial  uint32
	PageSequence     uint32
	Checksum         uint32
	SegmentTable     []byte
	SegmentData      []byte
	CRCVerified      bool
	RawPageBytesSize int
}

// IsContinuedPacket reports whether the first packet on this page is a continuation of a packet from the previous page.
func (p *Page) IsContinuedPacket() bool { return p.HeaderType&0x01 != 0 }

// IsBOS reports whether this page is the Beginning of Stream (BOS).
func (p *Page) IsBOS() bool { return p.HeaderType&0x02 != 0 }

// IsEOS reports whether this page is the End of Stream (EOS).
func (p *Page) IsEOS() bool { return p.HeaderType&0x04 != 0 }

// PageReader reads Ogg pages from an io.Reader.
type PageReader struct {
	r         *bufio.Reader
	VerifyCRC bool
	Resync    bool // If true, recover from stream desync by scanning for "OggS" (RFC 3533 §4)
	MaxResync int  // Maximum bytes to scan during resynchronization (default: 128KB)
	crcTable  [256]uint32
}

// crcTable8 contains the precomputed tables for Slice-by-8 CRC32 computation.
// Polynomial: 0x04C11DB7 (Ogg/Vorbis MSB-first, non-reflected).
var crcTable8 = func() [8][256]uint32 {
	var tbl [8][256]uint32
	const poly uint32 = 0x04C11DB7
	for i := 0; i < 256; i++ {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = (c << 1) ^ poly
			} else {
				c <<= 1
			}
		}
		tbl[0][i] = c
	}
	for k := 1; k < 8; k++ {
		for i := 0; i < 256; i++ {
			prev := tbl[k-1][i]
			tbl[k][i] = (prev << 8) ^ tbl[0][byte(prev>>24)]
		}
	}
	return tbl
}()

// NewPageReader creates a new PageReader wrapping an io.Reader with CRC verification and stream resync enabled.
func NewPageReader(r io.Reader) *PageReader {
	return &PageReader{
		r:         bufio.NewReaderSize(r, 128*1024),
		VerifyCRC: true,
		Resync:    true,
		MaxResync: 128 * 1024,
		crcTable:  crcTable8[0],
	}
}

// ReadPage reads the next Ogg page.
//
// If Resync is enabled (the default), ReadPage recovers from stream desynchronization
// or corrupted bytes by scanning forward for the "OggS" capture pattern per RFC 3533 §4.
func (pr *PageReader) ReadPage() (*Page, error) {
	var bytesSearched int
	var lastErr error
	maxResync := pr.MaxResync
	if maxResync <= 0 {
		maxResync = 128 * 1024
	}

	for {
		p4, err := pr.r.Peek(4)
		if err != nil {
			if errors.Is(err, io.EOF) && bytesSearched > 0 {
				if lastErr != nil {
					return nil, fmt.Errorf("%w: %w", ErrResyncFailed, lastErr)
				}
				return nil, ErrResyncFailed
			}
			return nil, err
		}

		if string(p4) != "OggS" {
			if !pr.Resync {
				var header [27]byte
				_, _ = io.ReadFull(pr.r, header[:])
				return nil, ErrInvalidCapturePattern
			}

			// Resynchronization: scan buffer for "OggS"
			bufSize := pr.r.Buffered()
			if bufSize < 4 {
				bufSize = 4
			}
			scanBuf, _ := pr.r.Peek(bufSize)
			idx := bytes.Index(scanBuf, []byte("OggS"))
			var discard int
			if idx > 0 {
				discard = idx
			} else if idx == -1 {
				discard = len(scanBuf) - 3
				if discard <= 0 {
					discard = 1
				}
			} else {
				discard = 1
			}

			if bytesSearched+discard > maxResync {
				return nil, fmt.Errorf("%w: exceeded max resync limit (%d bytes)", ErrResyncFailed, maxResync)
			}
			n, _ := pr.r.Discard(discard)
			bytesSearched += n
			lastErr = ErrInvalidCapturePattern
			continue
		}

		// Peek fixed 27-byte header
		hdr, err := pr.r.Peek(27)
		if err != nil {
			if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && pr.Resync {
				pr.r.Discard(1)
				bytesSearched++
				lastErr = err
				continue
			}
			return nil, err
		}

		version := hdr[4]
		if version != 0 {
			verErr := fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
			if !pr.Resync {
				pr.r.Discard(27)
				return nil, verErr
			}
			pr.r.Discard(1)
			bytesSearched++
			lastErr = verErr
			if bytesSearched > maxResync {
				return nil, fmt.Errorf("%w: %w", ErrResyncFailed, verErr)
			}
			continue
		}

		headerType := hdr[5]
		granule := binary.LittleEndian.Uint64(hdr[6:14])
		serial := binary.LittleEndian.Uint32(hdr[14:18])
		seq := binary.LittleEndian.Uint32(hdr[18:22])
		checksum := binary.LittleEndian.Uint32(hdr[22:26])
		pageSegments := int(hdr[26])

		hdrAndSegs, err := pr.r.Peek(27 + pageSegments)
		if err != nil {
			if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && pr.Resync {
				pr.r.Discard(1)
				bytesSearched++
				lastErr = err
				continue
			}
			return nil, err
		}

		bodyLen := 0
		for _, v := range hdrAndSegs[27 : 27+pageSegments] {
			bodyLen += int(v)
		}
		totalPageSize := 27 + pageSegments + bodyLen

		fullPage, err := pr.r.Peek(totalPageSize)
		if err != nil {
			if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && pr.Resync {
				pr.r.Discard(1)
				bytesSearched++
				lastErr = err
				continue
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				buf := make([]byte, totalPageSize)
				if _, err := io.ReadFull(pr.r, buf); err != nil {
					return nil, err
				}
				return pr.parsePageDirect(buf)
			}
			return nil, err
		}

		if pr.VerifyCRC {
			if !pr.verifySliceCRC(fullPage, checksum) {
				crcErr := fmt.Errorf("%w (serial=%d seq=%d)", ErrCRCMismatch, serial, seq)
				if !pr.Resync {
					pr.r.Discard(totalPageSize)
					return nil, crcErr
				}
				pr.r.Discard(1)
				bytesSearched++
				lastErr = crcErr
				if bytesSearched > maxResync {
					return nil, fmt.Errorf("%w: %w", ErrResyncFailed, crcErr)
				}
				continue
			}
		}

		// Discard verified page from buffer
		pr.r.Discard(totalPageSize)

		segTable := make([]byte, pageSegments)
		copy(segTable, fullPage[27:27+pageSegments])
		body := make([]byte, bodyLen)
		copy(body, fullPage[27+pageSegments:totalPageSize])

		return &Page{
			Version:          version,
			HeaderType:       headerType,
			GranulePosition:  granule,
			BitstreamSerial:  serial,
			PageSequence:     seq,
			Checksum:         checksum,
			SegmentTable:     segTable,
			SegmentData:      body,
			CRCVerified:      pr.VerifyCRC,
			RawPageBytesSize: totalPageSize,
		}, nil
	}
}

// updateCRC8 updates the MSB-first CRC32 using the Slice-by-8 algorithm.
func updateCRC8(crc uint32, data []byte) uint32 {
	t0 := &crcTable8[0]
	t1 := &crcTable8[1]
	t2 := &crcTable8[2]
	t3 := &crcTable8[3]
	t4 := &crcTable8[4]
	t5 := &crcTable8[5]
	t6 := &crcTable8[6]
	t7 := &crcTable8[7]

	for len(data) >= 8 {
		_ = data[7]
		v1 := binary.BigEndian.Uint32(data[:4]) ^ crc
		v2 := binary.BigEndian.Uint32(data[4:8])

		crc = t7[byte(v1>>24)] ^
			t6[byte(v1>>16)] ^
			t5[byte(v1>>8)] ^
			t4[byte(v1)] ^
			t3[byte(v2>>24)] ^
			t2[byte(v2>>16)] ^
			t1[byte(v2>>8)] ^
			t0[byte(v2)]

		data = data[8:]
	}

	for _, v := range data {
		crc = (crc << 8) ^ t0[byte(crc>>24)^v]
	}
	return crc
}

func (pr *PageReader) verifySliceCRC(page []byte, expected uint32) bool {
	if len(page) < 27 {
		return false
	}
	crc := updateCRC8(0, page[:22])
	zero4 := [4]byte{0, 0, 0, 0}
	crc = updateCRC8(crc, zero4[:])
	crc = updateCRC8(crc, page[26:])
	return crc == expected
}

func (pr *PageReader) parsePageDirect(fullPage []byte) (*Page, error) {
	if len(fullPage) < 27 {
		return nil, io.ErrUnexpectedEOF
	}
	checksum := binary.LittleEndian.Uint32(fullPage[22:26])
	serial := binary.LittleEndian.Uint32(fullPage[14:18])
	seq := binary.LittleEndian.Uint32(fullPage[18:22])
	if pr.VerifyCRC {
		if !pr.verifySliceCRC(fullPage, checksum) {
			return nil, fmt.Errorf("%w (serial=%d seq=%d)", ErrCRCMismatch, serial, seq)
		}
	}
	pageSegments := int(fullPage[26])
	segTable := make([]byte, pageSegments)
	copy(segTable, fullPage[27:27+pageSegments])
	body := make([]byte, len(fullPage)-(27+pageSegments))
	copy(body, fullPage[27+pageSegments:])

	return &Page{
		Version:          fullPage[4],
		HeaderType:       fullPage[5],
		GranulePosition:  binary.LittleEndian.Uint64(fullPage[6:14]),
		BitstreamSerial:  serial,
		PageSequence:     seq,
		Checksum:         checksum,
		SegmentTable:     segTable,
		SegmentData:      body,
		CRCVerified:      pr.VerifyCRC,
		RawPageBytesSize: len(fullPage),
	}, nil
}

func (pr *PageReader) verifyCRC(header [27]byte, segTable []byte, body []byte, expected uint32) (bool, error) {
	if len(header) != 27 {
		return false, errors.New("ogg: internal header size mismatch")
	}
	header[22] = 0
	header[23] = 0
	header[24] = 0
	header[25] = 0
	got := oggCRC3(header[:], segTable, body)
	return got == expected, nil
}

func oggCRC3(a []byte, b []byte, c []byte) uint32 {
	var crc uint32
	crc = updateCRC8(crc, a)
	crc = updateCRC8(crc, b)
	crc = updateCRC8(crc, c)
	return crc
}
