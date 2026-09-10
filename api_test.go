package opusgo

import (
	"math"
	"os"
	"testing"
)

func TestNewPlayerFromReader(t *testing.T) {
	f, err := os.Open("test/music_64kbps.opus")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	player, err := NewPlayerFromReader(f)
	if err != nil {
		t.Fatalf("NewPlayerFromReader: %v", err)
	}
	defer player.Close()

	buf := make([]byte, 4096)
	if _, err := player.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestNewPlayerF32FromReader(t *testing.T) {
	f, err := os.Open("test/music_64kbps.opus")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	player, err := NewPlayerF32FromReader(f)
	if err != nil {
		t.Fatalf("NewPlayerF32FromReader: %v", err)
	}
	defer player.Close()

	buf := make([]byte, 4096)
	if _, err := player.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestNewPlayerF32FromFile(t *testing.T) {
	player, err := NewPlayerF32FromFile("test/music_64kbps.opus", true)
	if err != nil {
		t.Fatalf("NewPlayerF32FromFile: %v", err)
	}
	defer player.Close()

	buf := make([]byte, 4096)
	if _, err := player.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestOpusPlayer_VolumeAndGainAPI(t *testing.T) {
	player, err := NewPlayerFromFile("test/music_64kbps.opus", true)
	if err != nil {
		t.Fatalf("NewPlayerFromFile: %v", err)
	}
	defer player.Close()

	if v := player.Volume(); v != 1.0 {
		t.Errorf("expected default volume 1.0, got %f", v)
	}
	if g := player.Gain(); math.Abs(g) > 1e-6 {
		t.Errorf("expected default gain ~0 dB, got %f", g)
	}

	player.SetVolume(0.5)
	if v := player.Volume(); v != 0.5 {
		t.Errorf("expected volume 0.5, got %f", v)
	}

	player.SetGain(0.0)
	if v := player.Volume(); math.Abs(v-1.0) > 1e-6 {
		t.Errorf("expected volume 1.0 for 0 dB, got %f", v)
	}
}
