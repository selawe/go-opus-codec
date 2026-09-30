package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/selawe/go-opus-codec/internal/atomicfile"
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
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
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

	var w = stdout
	var outFile *atomicfile.File
	if *out != "" {
		// Only made visible by Commit: a failure leaves no partial file and never
		// destroys an existing one, and the input can not be truncated by accident.
		outFile, err = atomicfile.Create(*out, fs.Arg(0))
		if err != nil {
			return fail(stderr, err)
		}
		defer outFile.Abort()
		w = outFile
	}
	bw := bufio.NewWriterSize(w, 128*1024)

	for {
		pkt, err := r.ReadAudioPacket()
		if errors.Is(err, io.EOF) {
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
	if err := bw.Flush(); err != nil {
		return fail(stderr, err)
	}
	if outFile != nil {
		if err := outFile.Commit(); err != nil {
			return fail(stderr, err)
		}
	}
	return 0
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
