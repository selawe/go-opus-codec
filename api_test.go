package opusgo

import (
	"math"
	"testing"
)

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
