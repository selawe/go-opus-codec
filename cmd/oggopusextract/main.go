package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/selawe/go-opus-codec/ogg"
)

// Writes length-prefixed Opus packets (u32le length + bytes) for audio packets only.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("oggopusextract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		noCRC = fs.Bool("no-crc", false, "skip Ogg CRC verification")
		out   = fs.String("out", "", "output file (default stdout)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: oggopusextract [--no-crc] [--out packets.bin] file.ogg")
		return 2
	}

	in, err := os.Open(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	defer in.Close()

	r, err := ogg.NewOpusReader(in)
	if err != nil {
		return fail(stderr, err)
	}
	r.SetVerifyCRC(!*noCRC)

	var w io.Writer = stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return fail(stderr, err)
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriterSize(w, 128*1024)
	defer bw.Flush()

	for {
		pkt, err := r.ReadAudioPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(stderr, err)
		}
		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(pkt.Data)))
		if _, err := bw.Write(lenBuf[:]); err != nil {
			return fail(stderr, err)
		}
		if _, err := bw.Write(pkt.Data); err != nil {
			return fail(stderr, err)
		}
	}
	return 0
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
