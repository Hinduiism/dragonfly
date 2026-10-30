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
func (spectatorControlsMode) AllowsInteraction() bool        { return true }

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
// to all viewers, including the player's own session. The broadcast must not
// reset the owning client's abilities after its local update.
func TestSpectatorControlsGameModePacketsKeepOwnHotbar(t *testing.T) {
	owner, _ := newPublicationSession("Observer", 16, nil)
	owner.entityRuntimeIDs[owner.ent] = selfEntityRuntimeID
	actor := &gameModeControllable{handle: owner.ent, mode: spectatorControlsMode{world.GameModeSpectator}, session: owner}
	owner.SendGameMode(actor)
	owner.ViewEntityGameMode(actor)

	var local, abilities bool
	packets := drainPublicationPackets(owner)
	for _, pk := range packets {
		switch pk := pk.(type) {
		case *packet.SetPlayerGameType:
			local = true
			if pk.GameType != packet.GameTypeSurvival {
				t.Fatalf("local game type = %d, want survival", pk.GameType)
			}
		case *packet.UpdatePlayerGameType:
			t.Fatalf("redundant self broadcast after abilities: %#v", pk)
		case *packet.UpdateAbilities:
			abilities = true
			var values uint32
			var forcedFlight bool
			for _, layer := range pk.AbilityData.Layers {
				if layer.Type == protocol.AbilityLayerTypeSpectator {
					forcedFlight = true
					if layer.Abilities != protocol.AbilityFlying || layer.Values != protocol.AbilityFlying {
						t.Fatalf("observer layer must force only flight, preserving hotbar interaction: %#v", layer)
					}
				}
				values |= layer.Values
			}
			want := uint32(protocol.AbilityNoClip | protocol.AbilityInvulnerable | protocol.AbilityMayFly)
			if values&want != want {
				t.Fatalf("observer protections missing: abilities=%x", values)
			}
			if !forcedFlight {
				t.Fatal("hotbar observer is missing its flight-only spectator layer")
			}
			if values&protocol.AbilityOpenContainers == 0 {
				t.Fatal("observer lost its control-item interaction ability")
			}
		}
	}
	if !local || !abilities {
		t.Fatalf("missing mode packets: local=%t abilities=%t", local, abilities)
	}
	last, ok := packets[len(packets)-1].(*packet.UpdateAbilities)
	if !ok || last.AbilityData.Layers[0].Values&protocol.AbilityFlying == 0 || !actor.Flying() {
		t.Fatalf("mode transition must finish with active flight abilities: last=%T flying=%t", packets[len(packets)-1], actor.Flying())
	}

	viewer, _ := newPublicationSession("Viewer", 16, nil)
	viewer.entityRuntimeIDs[owner.ent] = selfEntityRuntimeID + 1
	viewer.ViewEntityGameMode(actor)
	pk := packetOf[*packet.UpdatePlayerGameType](t, viewer)
	if pk.PlayerUniqueID != selfEntityRuntimeID+1 || pk.GameType != packet.GameTypeSpectator {
		t.Fatalf("other viewer must still see spectator: %#v", pk)
	}
}

func TestObserverAbilityLayersReturnToNormalOnExit(t *testing.T) {
	owner, _ := newPublicationSession("Observer", 16, nil)
	owner.entityRuntimeIDs[owner.ent] = selfEntityRuntimeID
	actor := &gameModeControllable{handle: owner.ent, mode: spectatorControlsMode{world.GameModeSpectator}, session: owner}
	owner.SendGameMode(actor)
	_ = drainPublicationPackets(owner)

	actor.mode = world.GameModeSurvival
	actor.flying = false
	owner.SendGameMode(actor)
	owner.ViewEntityGameMode(actor)
	packets := drainPublicationPackets(owner)
	if len(packets) != 2 {
		t.Fatalf("exit packets = %d, want local mode and abilities", len(packets))
	}
	pk, ok := packets[1].(*packet.UpdateAbilities)
	if !ok || len(pk.AbilityData.Layers) != 1 || pk.AbilityData.Layers[0].Type != protocol.AbilityLayerTypeBase {
		t.Fatalf("exit retained observer ability layers: %#v", packets[1])
	}
	wantOff := uint32(protocol.AbilityFlying | protocol.AbilityMayFly | protocol.AbilityNoClip | protocol.AbilityInvulnerable)
	if pk.AbilityData.Layers[0].Values&wantOff != 0 {
		t.Fatalf("survival retained observer abilities: %#v", pk.AbilityData.Layers[0])
	}
}

type gameModeControllable struct {
	Controllable
	handle  *world.EntityHandle
	mode    world.GameMode
	flying  bool
	session *Session
}

func (c *gameModeControllable) H() *world.EntityHandle   { return c.handle }
func (*gameModeControllable) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (*gameModeControllable) Rotation() cube.Rotation    { return cube.Rotation{} }
func (c *gameModeControllable) GameMode() world.GameMode { return c.mode }
func (c *gameModeControllable) Flying() bool             { return c.flying }
func (c *gameModeControllable) StartFlying() {
	if !c.mode.AllowsFlying() || c.flying {
		return
	}
	c.flying = true
	if c.session != nil {
		c.session.SendGameMode(c)
	}
}
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
