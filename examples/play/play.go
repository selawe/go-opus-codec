package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/selawe/go-opus-codec"
)

// source is what both player flavours (int16 and float32) expose to oto.
type source interface {
	Read(p []byte) (int, error)
	IsFinished() bool
	Close() error
}

// playSource plays src through the default audio device until it is exhausted or the
// device reports an error (for example a read or decode failure inside the stream).
func playSource(src source, format oto.Format, bytesPerSample int) error {
	var options oto.NewContextOptions
	options.SampleRate = 48000
	options.ChannelCount = 2
	options.Format = format

	context, ready, err := oto.NewContext(&options)
	if err != nil {
		return err
	}

	log.Printf("Waiting for audio context to be ready")
	<-ready

	log.Printf("Playing")

	otoPlayer := context.NewPlayer(src)
	otoPlayer.SetBufferSize(options.SampleRate * bytesPerSample * options.ChannelCount / 4) // 250ms buffer
	otoPlayer.Play()

	// oto pulls from src on its own goroutine and stops when Read returns an error, at
	// which point IsFinished may never become true: poll Err so the failure is reported
	// instead of the loop spinning forever.
	for !src.IsFinished() {
		if err := otoPlayer.Err(); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}

	// wait just a bit longer for the last audio packet to finish playing
	time.Sleep(500 * time.Millisecond)
	return otoPlayer.Err()
}

func play(filename string) error {
	fmt.Printf("Playing file: %s\n", filename)

	opusPlayer, err := opusgo.NewPlayerFromFile(filename, true)
	if err != nil {
		return err
	}
	defer opusPlayer.Close()

	return playSource(opusPlayer, oto.FormatSignedInt16LE, 2)
}

func playF32(filename string) error {
	fmt.Printf("Playing file: %s\n", filename)

	opusPlayer, err := opusgo.NewPlayerF32FromFile(filename, true)
	if err != nil {
		return err
	}
	defer opusPlayer.Close()

	return playSource(opusPlayer, oto.FormatFloat32LE, 4)
}

func main() {
	log.SetFlags(log.Ldate | log.Lshortfile | log.Lmicroseconds)
	int16Out := flag.Bool("int16", false, "play 16-bit integer samples instead of float32")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: play [-int16] <file.ogg|file.opus>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	run := playF32
	if *int16Out {
		run = play
	}
	if err := run(flag.Arg(0)); err != nil {
		fmt.Fprintf(os.Stderr, "Error playing file: %v\n", err)
		os.Exit(1)
	}

	log.Println("Exiting")
}
