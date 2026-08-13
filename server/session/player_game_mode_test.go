package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestGameTypeFromMode(t *testing.T) {
	tests := []struct {
		name string
		mode world.GameMode
		want int32
	}{
		{name: "survival", mode: world.GameModeSurvival, want: packet.GameTypeSurvival},
		{name: "creative", mode: world.GameModeCreative, want: packet.GameTypeCreative},
		{name: "adventure", mode: world.GameModeAdventure, want: packet.GameTypeSurvival},
		{name: "spectator", mode: world.GameModeSpectator, want: packet.GameTypeSpectator},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gameTypeFromMode(test.mode); got != test.want {
				t.Fatalf("game type: got %v, want %v", got, test.want)
			}
		})
	}
}
