package ogg_test

import (
	"bytes"
	"fmt"
	"log"

	"github.com/selawe/go-opus-codec/ogg"
)

// Write a minimal Ogg Opus stream and read it back. RFC 7845 wants OpusHead and OpusTags each on a
// page of their own, so they are flushed explicitly; audio then starts on a fresh page.
func ExampleOpusReader() {
	var buf bytes.Buffer
	pw := ogg.NewPacketWriter(&buf, 0x1234)

	head, err := ogg.BuildOpusHeadPacket(ogg.OpusHead{Version: 1, Channels: 2, PreSkip: 312, InputSampleRate: 48000})
	if err != nil {
		log.Fatal(err)
	}
	tags, err := ogg.BuildOpusTagsPacket(ogg.OpusTags{Vendor: "example", Comments: []string{"TITLE=Demo"}})
	if err != nil {
		log.Fatal(err)
	}
	for i, p := range [][]byte{head, tags} {
		if err := pw.WritePacket(p, 0, i == 0, false); err != nil {
			log.Fatal(err)
		}
		if err := pw.FlushPage(); err != nil {
			log.Fatal(err)
		}
	}
	// One audio packet (a CELT TOC byte plus a payload byte), the last one, so it ends the stream.
	if err := pw.WritePacket([]byte{0xFC, 0x01}, 312+960, false, true); err != nil {
		log.Fatal(err)
	}
	if err := pw.Flush(); err != nil {
		log.Fatal(err)
	}

	r, err := ogg.NewOpusReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("channels:", r.Head.Channels, "pre-skip:", r.Head.PreSkip)
	fmt.Println("tags:", r.Tags.Vendor, r.Tags.Comments)

	pkt, err := r.ReadAudioPacket()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("audio packet bytes:", len(pkt.Data), "last:", pkt.EOS)
	// Output:
	// channels: 2 pre-skip: 312
	// tags: example [TITLE=Demo]
	// audio packet bytes: 2 last: true
}
