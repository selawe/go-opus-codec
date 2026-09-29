package main

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/selawe/go-opus-codec/ogg"
	"github.com/selawe/go-opus-codec/opus"
	"github.com/selawe/go-opus-codec/resample"
)

type decodeChunk struct {
	b   []byte
	err error
}

// chunkChanReader adapts a channel of decoded PCM byte chunks into an io.Reader.
// It returns the terminal error (often io.EOF) only after all buffered bytes are read.
type chunkChanReader struct {
	ch       <-chan decodeChunk
	buf      []byte
	finalErr error
}

func (r *chunkChanReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) == 0 {
		if r.finalErr != nil {
			return 0, r.finalErr
		}
		chunk, ok := <-r.ch
		if !ok {
			return 0, io.EOF
		}
		r.buf = chunk.b
		r.finalErr = chunk.err
		if len(r.buf) == 0 {
			if r.finalErr != nil {
				return 0, r.finalErr
			}
			return 0, nil
		}
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func formatDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

type progressReporter struct {
	decodedLenBytes     int64
	totalFramesEstimate int64
	start               time.Time
	lastPrint           time.Time
	spinner             []byte
	spin                int
}

func newProgressReporter(decodedLenBytes, totalFramesEstimate int64) *progressReporter {
	now := time.Now()
	return &progressReporter{
		decodedLenBytes:     decodedLenBytes,
		totalFramesEstimate: totalFramesEstimate,
		start:               now,
		lastPrint:           now,
		spinner:             []byte{'|', '/', '-', '\\'},
	}
}

func (p *progressReporter) Print(framesDone, bytesRead int64, force bool) {
	if p == nil {
		return
	}
	if !force {
		if time.Since(p.lastPrint) < 100*time.Millisecond {
			return
		}
	}
	p.lastPrint = time.Now()

	elapsed := time.Since(p.start)
	ch := p.spinner[p.spin%len(p.spinner)]
	p.spin++

	if p.totalFramesEstimate > 0 {
		pct := float64(framesDone) / float64(p.totalFramesEstimate) * 100
		if pct > 100 {
			pct = 100
		}
		etaStr := "--:--"
		if framesDone > 0 && elapsed > 0 {
			rate := float64(framesDone) / elapsed.Seconds()
			if rate > 0 {
				remaining := float64(p.totalFramesEstimate - framesDone)
				if remaining < 0 {
					remaining = 0
				}
				eta := time.Duration(remaining / rate * float64(time.Second))
				etaStr = formatDur(eta)
			}
		}
		fmt.Fprintf(os.Stderr, "\r%c %6.2f%% (%d/%d frames) elapsed %s eta %s", ch, pct, framesDone, p.totalFramesEstimate, formatDur(elapsed), etaStr)
		return
	}

	if p.decodedLenBytes > 0 {
		pct := float64(bytesRead) / float64(p.decodedLenBytes) * 100
		if pct > 100 {
			pct = 100
		}
		etaStr := "--:--"
		if bytesRead > 0 && elapsed > 0 {
			rate := float64(bytesRead) / elapsed.Seconds()
			if rate > 0 {
				remaining := float64(p.decodedLenBytes - bytesRead)
				if remaining < 0 {
					remaining = 0
				}
				eta := time.Duration(remaining / rate * float64(time.Second))
				etaStr = formatDur(eta)
			}
		}
		fmt.Fprintf(os.Stderr, "\r%c %6.2f%% (%d bytes) elapsed %s eta %s", ch, pct, bytesRead, formatDur(elapsed), etaStr)
		return
	}

	fmt.Fprintf(os.Stderr, "\r%c %d frames elapsed %s", ch, framesDone, formatDur(elapsed))
}

// pcmResampler turns the decoder's s16le byte stream into fixed-size frames at the Opus rate,
// resampling with the library's polyphase windowed-sinc filter (package resample) when the
// input rate differs from the output rate.
type pcmResampler struct {
	r        io.Reader
	channels int
	rs       *resample.Resampler // nil when no rate conversion is needed

	queue []int16 // converted frames not yet handed out
	carry []int16 // trailing samples of an incomplete frame, kept for the next read
	odd   []byte  // a dangling half sample, kept for the next read
	tmp   []byte

	eof       bool
	bytesRead int64
}

func newPCMResampler(r io.Reader, inRate, outRate, channels int) *pcmResampler {
	p := &pcmResampler{r: r, channels: channels, tmp: make([]byte, 32*1024)}
	if inRate != outRate {
		p.rs = resample.New(channels, inRate, outRate)
	}
	return p
}

func (p *pcmResampler) BytesRead() int64 {
	if p == nil {
		return 0
	}
	return p.bytesRead
}

// readMore reads one chunk of decoded PCM, converts it and appends the result to the queue.
// At end of input it flushes the resampler so no tail samples are lost.
func (p *pcmResampler) readMore() error {
	n, err := p.r.Read(p.tmp)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	p.bytesRead += int64(n)

	data := append(p.odd, p.tmp[:n]...)
	p.odd = append(p.odd[:0], data[len(data)&^1:]...) // keep a dangling half sample
	data = data[:len(data)&^1]

	samples := append(p.carry, make([]int16, len(data)/2)...)[:len(p.carry)+len(data)/2]
	for i := 0; i+1 < len(data); i += 2 {
		samples[len(p.carry)+i/2] = int16(binary.LittleEndian.Uint16(data[i:]))
	}
	whole := len(samples) / p.channels * p.channels
	p.carry = append(p.carry[:0], samples[whole:]...)
	samples = samples[:whole]

	if p.rs != nil {
		samples = p.rs.ProcessInt16(samples)
	}
	p.queue = append(p.queue, samples...)

	if errors.Is(err, io.EOF) {
		if p.rs != nil {
			p.queue = append(p.queue, p.rs.FlushInt16()...)
		}
		p.eof = true
	}
	return nil
}

// Fill fills outPCM with exactly outFrames frames at the output rate, padding with silence past
// the end of the input. It returns how many of those frames contain actual audio, and whether
// this call consumed the last of the input (done=true).
func (p *pcmResampler) Fill(outPCM []int16, outFrames int) (actualFrames int, done bool, err error) {
	if p == nil {
		return 0, true, errors.New("resampler: nil")
	}
	if outFrames <= 0 || len(outPCM) < outFrames*p.channels {
		return 0, false, errors.New("resampler: invalid output buffer")
	}
	need := outFrames * p.channels
	for len(p.queue) < need && !p.eof {
		if err := p.readMore(); err != nil {
			return 0, false, err
		}
	}
	n := copy(outPCM[:need], p.queue)
	clear(outPCM[n:need])
	p.queue = append(p.queue[:0], p.queue[n:]...)
	return n / p.channels, p.eof && len(p.queue) == 0, nil
}

func main() {
	const (
		channels       = 2
		frameSize48k   = 960 // 20ms @ 48kHz
		bitrate        = 64000
		vbr            = true
		complexity     = 10
		opusSampleRate = ogg.OpusSampleRateHz
	)

	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s input.mp3 [output.opus]\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}

	inPath := os.Args[1]
	outPath := ""
	if len(os.Args) == 3 {
		outPath = os.Args[2]
	} else {
		outPath = defaultOutPath(inPath)
	}

	inF, err := os.Open(inPath)
	if err != nil {
		fatal(err)
	}
	defer inF.Close()

	// need a seekable version first to get the length
	dec, err := mp3.NewDecoder(inF)
	if err != nil {
		fatal(err)
	}

	inSampleRate := dec.SampleRate()
	if inSampleRate <= 0 {
		fatal(fmt.Errorf("mp3: invalid sample rate: %d", inSampleRate))
	}

	decodedLenBytes := dec.Length()
	var totalFramesEstimate int64
	if decodedLenBytes > 0 {
		inFrames := decodedLenBytes / int64(channels*2)
		if inFrames > 0 {
			outFrames := (inFrames*int64(opusSampleRate) + int64(inSampleRate) - 1) / int64(inSampleRate)
			totalFramesEstimate = (outFrames + int64(frameSize48k) - 1) / int64(frameSize48k)
		}
	}
	progress := newProgressReporter(decodedLenBytes, totalFramesEstimate)

	inF.Seek(0, io.SeekStart)
	// then a buffered version for decoding
	dec, _ = mp3.NewDecoder(bufio.NewReader(inF))

	// Decode MP3 PCM in a separate goroutine and feed bytes through a buffered channel.
	// This lets CPU-heavy decode and encode overlap.
	chunks := make(chan decodeChunk, 16)
	var producedBytes atomic.Int64
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := dec.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				producedBytes.Add(int64(n))
				chunks <- decodeChunk{b: b}
			}
			if err != nil {
				chunks <- decodeChunk{err: err}
				return
			}
		}
	}()
	decodedReader := &chunkChanReader{ch: chunks}

	enc, err := opus.NewEncoder(opusSampleRate, channels, opus.ApplicationAudio)
	if err != nil {
		fatal(fmt.Errorf("unable to create opus encoder: %v", err))
	}
	defer enc.Close()

	if err := enc.SetBitrate(bitrate); err != nil {
		fatal(err)
	}
	if err := enc.SetVBR(vbr); err != nil {
		fatal(err)
	}
	if err := enc.SetComplexity(complexity); err != nil {
		fatal(err)
	}

	lookahead, err := enc.Lookahead()
	if err != nil {
		fatal(err)
	}

	outF, err := os.Create(outPath)
	if err != nil {
		fatal(err)
	}
	defer func() {
		_ = outF.Close()
	}()

	outBW := bufio.NewWriterSize(outF, 1<<20)
	defer func() {
		_ = outBW.Flush()
	}()

	serial := randomSerial()
	pw := ogg.NewPacketWriter(outBW, serial)
	// Batch packets into pages of about one second instead of one page per packet,
	// which would add a 27+ byte page header to every packet.
	pw.MaxPageSize = 8 << 10
	pw.MaxPagePackets = max(1, opusSampleRate/frameSize48k)

	head := ogg.OpusHead{
		Version:              1,
		Channels:             uint8(channels),
		PreSkip:              uint16(lookahead),
		InputSampleRate:      uint32(inSampleRate),
		OutputGainQ8:         0,
		ChannelMappingFamily: 0,
	}
	headPkt, err := ogg.BuildOpusHeadPacket(head)
	if err != nil {
		fatal(err)
	}

	tags := ogg.OpusTags{
		Vendor:   "opusgo",
		Comments: []string{"ENCODER=github.com/selawe/go-opus-codec", "ENCODED=" + time.Now().UTC().Format(time.RFC3339)},
	}
	tagsPkt, err := ogg.BuildOpusTagsPacket(tags)
	if err != nil {
		fatal(err)
	}

	if err := pw.WritePacket(headPkt, 0, true, false); err != nil {
		fatal(err)
	}
	if err := pw.WritePacket(tagsPkt, 0, false, false); err != nil {
		fatal(err)
	}
	// RFC 7845 Section 3: OpusTags must end its page and audio starts on a fresh one.
	if err := pw.FlushPage(); err != nil {
		fatal(err)
	}

	resampler := newPCMResampler(decodedReader, inSampleRate, opusSampleRate, channels)

	pcm := make([]int16, frameSize48k*channels)
	packet := make([]byte, 4000)
	var totalSamplesPerCh48k uint64 // input samples, at 48 kHz
	var framesDone int64
	inputDone := false

	for {
		framesActual := 0
		if !inputDone {
			var done bool
			var err error
			framesActual, done, err = resampler.Fill(pcm, frameSize48k)
			if err != nil {
				fatal(err)
			}
			inputDone = done
			totalSamplesPerCh48k += uint64(framesActual)
		} else {
			clear(pcm) // flush the encoder lookahead with silence
		}

		nBytes, err := enc.Encode(pcm, frameSize48k, packet)
		if err != nil {
			fatal(err)
		}

		framesDone++
		progress.Print(framesDone, resampler.BytesRead(), false)

		// The encoder delays its output by the lookahead (the pre-skip), so keep encoding
		// after the input ends until every input sample has come out of it.
		encoded := uint64(framesDone) * uint64(frameSize48k)
		needed := totalSamplesPerCh48k + uint64(head.PreSkip)
		isLast := inputDone && encoded >= needed

		// RFC 7845 Section 4: granule counts all decoded samples (pre-skip included), so
		// interior pages carry the encoded sample count. Only the final page is trimmed,
		// to PreSkip + input length.
		granule := encoded
		if isLast {
			granule = needed
		}

		if err := pw.WritePacket(packet[:nBytes], granule, false, isLast); err != nil {
			fatal(err)
		}
		if isLast {
			break
		}
	}

	progress.Print(framesDone, resampler.BytesRead(), true)
	fmt.Fprintln(os.Stderr)
	fmt.Printf("Wrote %s\n", outPath)

	if err := pw.Flush(); err != nil {
		fatal(err)
	}
}

func defaultOutPath(inPath string) string {
	base := strings.TrimSuffix(inPath, filepath.Ext(inPath))
	if base == "" {
		return inPath + ".opus"
	}
	return base + ".opus"
}

func randomSerial() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return binary.LittleEndian.Uint32(b[:])
	}
	return uint32(time.Now().UnixNano())
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
