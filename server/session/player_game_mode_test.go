package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type spectatorControlsMode struct{ world.GameMode }

func (spectatorControlsMode) ClientGameMode() world.GameMode { return world.GameModeSurvival }

func TestSpectatorControlsOnlyChangeOwnClientPresentation(t *testing.T) {
	mode := spectatorControlsMode{world.GameModeSpectator}
	if got := clientGameTypeFromMode(mode); got != packet.GameTypeSurvival {
		t.Fatalf("own client mode = %d, want hotbar-compatible survival", got)
	}
	if got := gameTypeFromMode(mode); got != packet.GameTypeSpectator {
		t.Fatalf("viewer mode = %d, want spectator", got)
	}
}

// SetGameMode sends the local game type, then broadcasts the actor game type
// to all viewers, including the player's own session. Both must agree for self.
func TestSpectatorControlsGameModePacketsKeepOwnHotbar(t *testing.T) {
	owner, _ := newPublicationSession("Observer", 16, nil)
	owner.entityRuntimeIDs[owner.ent] = selfEntityRuntimeID
	actor := &gameModeControllable{handle: owner.ent, mode: spectatorControlsMode{world.GameModeSpectator}}
	owner.SendGameMode(actor)
	owner.ViewEntityGameMode(actor)

	var local, update, abilities bool
	for _, pk := range drainPublicationPackets(owner) {
		switch pk := pk.(type) {
		case *packet.SetPlayerGameType:
			local = true
			if pk.GameType != packet.GameTypeSurvival {
				t.Fatalf("local game type = %d, want survival", pk.GameType)
			}
		case *packet.UpdatePlayerGameType:
			update = true
			if pk.PlayerUniqueID != selfEntityRuntimeID || pk.GameType != packet.GameTypeSurvival {
				t.Fatalf("self broadcast overwrites hotbar presentation: %#v", pk)
			}
		case *packet.UpdateAbilities:
			abilities = true
			var values uint32
			for _, layer := range pk.AbilityData.Layers {
				if layer.Type == protocol.AbilityLayerTypeSpectator {
					t.Fatal("hotbar observer received a native spectator layer")
				}
				values |= layer.Values
			}
			want := uint32(protocol.AbilityNoClip | protocol.AbilityInvulnerable | protocol.AbilityMayFly)
			if values&want != want {
				t.Fatalf("observer protections missing: abilities=%x", values)
			}
		}
	}
	if !local || !update || !abilities {
		t.Fatalf("missing mode packets: local=%t update=%t abilities=%t", local, update, abilities)
	}

	viewer, _ := newPublicationSession("Viewer", 16, nil)
	viewer.entityRuntimeIDs[owner.ent] = selfEntityRuntimeID + 1
	viewer.ViewEntityGameMode(actor)
	pk := packetOf[*packet.UpdatePlayerGameType](t, viewer)
	if pk.PlayerUniqueID != selfEntityRuntimeID+1 || pk.GameType != packet.GameTypeSpectator {
		t.Fatalf("other viewer must still see spectator: %#v", pk)
	}
}

type gameModeControllable struct {
	Controllable
	handle *world.EntityHandle
	mode   world.GameMode
	flying bool
}

func (c *gameModeControllable) H() *world.EntityHandle     { return c.handle }
func (*gameModeControllable) Position() mgl64.Vec3         { return mgl64.Vec3{} }
func (*gameModeControllable) Rotation() cube.Rotation      { return cube.Rotation{} }
func (c *gameModeControllable) GameMode() world.GameMode   { return c.mode }
func (c *gameModeControllable) Flying() bool               { return c.flying }
func (c *gameModeControllable) StartFlying()               { c.flying = true }
func (*gameModeControllable) FlightSpeed() float64         { return 0.05 }
func (*gameModeControllable) VerticalFlightSpeed() float64 { return 1 }

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
