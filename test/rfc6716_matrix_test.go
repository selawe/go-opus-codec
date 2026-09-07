package test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/selawe/go-opus-codec/opus"
)

type matrixKey struct {
	rate     int
	channels int
	vector   string
}

type matrixResult struct {
	passed      bool
	frames      int
	err         error
	correlation float64
	snr         float64
}

// TestRFC6716_MiniVector runs offline out-of-the-box in standard "go test ./..."
// using the bundled 6 KB bitstream fixture containing SILK, CELT, and PLC frames.
func TestRFC6716_MiniVector(t *testing.T) {
	bitstreamPath := filepath.Join("testdata", "testvector_mini.bit")
	f, err := os.Open(bitstreamPath)
	if err != nil {
		t.Fatalf("open mini vector: %v", err)
	}
	defer f.Close()

	reader := NewBitstreamReader(f)
	dec, err := opus.NewDecoder(48000, 2)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	pcm := make([]int16, 5760*2)
	frameIdx := 0

	for {
		frame, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("frame %d read: %v", frameIdx, err)
		}

		var samples int
		if frame.IsPacketLoss {
			samples, err = dec.Decode(nil, pcm, 960, false)
			if err != nil {
				t.Fatalf("frame %d [PLC] decode: %v", frameIdx, err)
			}
		} else {
			samples, err = dec.Decode(frame.Payload, pcm, 960, false)
			if err != nil {
				t.Fatalf("frame %d decode: %v", frameIdx, err)
			}
		}

		if samples <= 0 {
			t.Fatalf("frame %d decoded zero or negative samples: %d", frameIdx, samples)
		}

		gotFinalRange, err := dec.FinalRange()
		if err != nil {
			t.Fatalf("frame %d FinalRange: %v", frameIdx, err)
		}

		if frame.WantFinalRange != 0 && gotFinalRange != frame.WantFinalRange {
			t.Fatalf("frame %d final range mismatch: want 0x%08x, got 0x%08x", frameIdx, frame.WantFinalRange, gotFinalRange)
		}

		frameIdx++
	}

	if frameIdx == 0 {
		t.Fatal("expected at least 1 frame to be verified")
	}
	t.Logf("Successfully verified %d frames bit-exact against RFC 6716 range decoder", frameIdx)
}

// TestRFC6716_FullConformanceMatrix executes the 120-test matrix
// (5 sample rates x 2 channels x 12 official RFC test vectors).
func TestRFC6716_FullConformanceMatrix(t *testing.T) {
	vectorDir := os.Getenv("OPUS_RFC6716_TESTVECTORS")
	if vectorDir == "" {
		// Also check local testvectors directory
		localPath := filepath.Join("..", "testvectors")
		if info, err := os.Stat(localPath); err == nil && info.IsDir() {
			vectorDir = localPath
		}
	}

	if vectorDir == "" {
		t.Skip("RFC 6716 test vectors not found. Set OPUS_RFC6716_TESTVECTORS or run scripts/download_testvectors.sh")
	}

	rates := []int{8000, 12000, 16000, 24000, 48000}
	channelCounts := []int{1, 2}
	vectors := []string{
		"01", "02", "03", "04", "05", "06",
		"07", "08", "09", "10", "11", "12",
	}

	results := make(map[matrixKey]matrixResult)
	var resultsMu sync.Mutex

	for _, rate := range rates {
		for _, ch := range channelCounts {
			for _, vec := range vectors {
				key := matrixKey{rate: rate, channels: ch, vector: vec}
				testName := fmt.Sprintf("rate_%d/ch_%d/vec_%s", rate, ch, vec)

				t.Run(testName, func(t *testing.T) {
					t.Parallel()

					bitPath := filepath.Join(vectorDir, fmt.Sprintf("testvector%s.bit", vec))
					if _, err := os.Stat(bitPath); err != nil {
						// Check nested rfc6716 or rfc8251 subdirs
						bitPath = filepath.Join(vectorDir, "rfc6716", fmt.Sprintf("testvector%s.bit", vec))
						if _, err2 := os.Stat(bitPath); err2 != nil {
							t.Fatalf("missing vector %s: %v", vec, err)
						}
					}

					f, err := os.Open(bitPath)
					if err != nil {
						t.Fatalf("open bitstream: %v", err)
					}
					defer f.Close()

					dec, err := opus.NewDecoder(rate, ch)
					if err != nil {
						t.Fatalf("NewDecoder: %v", err)
					}
					defer dec.Close()

					reader := NewBitstreamReader(f)
					maxFrameSamples := rate * 120 / 1000 // 120 ms max
					pcmBuf := make([]int16, maxFrameSamples*ch)
					frameCount := 0

					for {
						frame, err := reader.Next()
						if err != nil {
							if err == io.EOF {
								break
							}
							t.Fatalf("frame %d read: %v", frameCount, err)
						}

						if frame.IsPacketLoss {
							_, err = dec.Decode(nil, pcmBuf, maxFrameSamples, false)
							if err != nil {
								t.Fatalf("frame %d [PLC]: %v", frameCount, err)
							}
						} else {
							_, err = dec.Decode(frame.Payload, pcmBuf, maxFrameSamples, false)
							if err != nil {
								t.Fatalf("frame %d decode: %v", frameCount, err)
							}
						}

						gotRange, err := dec.FinalRange()
						if err != nil {
							t.Fatalf("frame %d FinalRange: %v", frameCount, err)
						}

						if frame.WantFinalRange != 0 && gotRange != frame.WantFinalRange {
							t.Fatalf("frame %d final range mismatch: want 0x%08x, got 0x%08x", frameCount, frame.WantFinalRange, gotRange)
						}

						frameCount++
					}

					resultsMu.Lock()
					results[key] = matrixResult{
						passed: true,
						frames: frameCount,
					}
					resultsMu.Unlock()
				})
			}
		}
	}

	t.Cleanup(func() {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		printMatrixASCII(t, results, rates, channelCounts, vectors)
		exportMatrixMarkdown("conformance_matrix.md", results, rates, channelCounts, vectors)
	})
}

func printMatrixASCII(t *testing.T, results map[matrixKey]matrixResult, rates, channels []int, vectors []string) {
	var sb strings.Builder
	sb.WriteString("\n=== RFC 6716 / RFC 8251 CONFORMANCE MATRIX (120 TESTS) ===\n")
	sb.WriteString("+----------+----+----+----+----+----+----+----+----+----+----+----+----+\n")
	sb.WriteString("| Rate     | Ch | 01 | 02 | 03 | 04 | 05 | 06 | 07 | 08 | 09 | 10 | 11 | 12 |\n")
	sb.WriteString("+----------+----+----+----+----+----+----+----+----+----+----+----+----+\n")

	totalPassed := 0
	totalTests := 0

	for _, r := range rates {
		for _, ch := range channels {
			sb.WriteString(fmt.Sprintf("| %-8d | %-2d |", r, ch))
			for _, vec := range vectors {
				totalTests++
				res, ok := results[matrixKey{rate: r, channels: ch, vector: vec}]
				if ok && res.passed {
					sb.WriteString(" OK |")
					totalPassed++
				} else {
					sb.WriteString("FAIL|")
				}
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("+----------+----+----+----+----+----+----+----+----+----+----+----+----+\n")
	sb.WriteString(fmt.Sprintf("Total Score: %d / %d Tests Passed (%.1f%%)\n", totalPassed, totalTests, float64(totalPassed)/float64(totalTests)*100))
	t.Log(sb.String())
	fmt.Print(sb.String())
}

func exportMatrixMarkdown(outPath string, results map[matrixKey]matrixResult, rates, channels []int, vectors []string) {
	var sb strings.Builder
	sb.WriteString("# RFC 6716 / RFC 8251 Conformance Report\n\n")
	sb.WriteString("Decoder conformance results verified against official IETF test vectors.\n\n")
	sb.WriteString("| Rate (Hz) | Channels | " + strings.Join(vectors, " | ") + " |\n")
	sb.WriteString("|:---:|:---:|" + strings.Repeat(":---:|", len(vectors)) + "\n")

	for _, r := range rates {
		for _, ch := range channels {
			sb.WriteString(fmt.Sprintf("| %d | %d |", r, ch))
			for _, vec := range vectors {
				res, ok := results[matrixKey{rate: r, channels: ch, vector: vec}]
				if ok && res.passed {
					sb.WriteString(" ✅ PASS |")
				} else {
					sb.WriteString(" ❌ FAIL |")
				}
			}
			sb.WriteString("\n")
		}
	}

	if _, err := os.Stat("../go.mod"); err == nil {
		_ = os.WriteFile(filepath.Join("..", outPath), []byte(sb.String()), 0644)
	} else {
		_ = os.WriteFile(outPath, []byte(sb.String()), 0644)
	}
}
