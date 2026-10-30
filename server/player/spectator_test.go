package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type controlsSpectatorMode struct{ world.GameMode }

func (controlsSpectatorMode) AllowsInteraction() bool        { return true }
func (controlsSpectatorMode) ClientGameMode() world.GameMode { return world.GameModeSurvival }

func TestNativeSpectatorTransitionStopsUseGlidingAndContainer(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
		p.UseItem()
		p.gliding = true
		closed := false
		if err := p.OpenVirtualChest(p.tx, VirtualContainerConfig{
			Inventory: inventory.New(27, nil), OnClose: func(*world.Tx, *Player) { closed = true },
		}); err != nil {
			t.Fatal(err)
		}
		p.SetGameMode(world.GameModeSpectator)
		if p.UsingItem() || p.Gliding() || !p.Flying() || p.Invisible() || !closed {
			t.Fatalf("transition: using=%v gliding=%v flying=%v invisible=%v containerClosed=%v", p.UsingItem(), p.Gliding(), p.Flying(), p.Invisible(), closed)
		}
		p.UseItem()
		held, _ := p.HeldItems()
		if p.UsingItem() || held.Count() != 2 || p.Food() != 10 {
			t.Fatal("native spectator consumed or started using food")
		}
	})
}

func TestSpectatorModeVisibilityDoesNotStickOrClearExplicitInvisibility(t *testing.T) {
	withDamageTestPlayer(t, func(p *Player) {
		controls := controlsSpectatorMode{world.GameModeSpectator}
		p.SetGameMode(controls)
		if !p.Invisible() {
			t.Fatal("hotbar observer metadata is visible")
		}
		p.SetVisible()
		if !p.Invisible() {
			t.Fatal("ordinary reveal bypassed observer mode visibility")
		}
		p.SetGameMode(world.GameModeSpectator)
		if p.Invisible() {
			t.Fatal("native spectator retained the variant's metadata invisibility")
		}
		p.SetGameMode(controls)
		p.AddEffect(effect.New(effect.Invisibility, 1, time.Minute))
		p.SetGameMode(world.GameModeSpectator)
		if !p.Invisible() {
			t.Fatal("mode switch cleared invisibility effect")
		}
		p.RemoveEffect(effect.Invisibility)
		if p.Invisible() {
			t.Fatal("expired invisibility remained stuck in native spectator")
		}
		p.SetGameMode(world.GameModeSurvival)
		if p.Invisible() {
			t.Fatal("ordinary player remained invisible after mode return")
		}
	})
}

func TestSpectatorModesKeepFlightAndSkipBlockEffects(t *testing.T) {
	for _, mode := range []world.GameMode{world.GameModeSpectator, controlsSpectatorMode{world.GameModeSpectator}} {
		withStateTestPlayer(t, func(p *Player, _ *stateConn) {
			p.SetGameMode(mode)
			flags := protocol.NewInputFlags(packet.InputFlagCount)
			flags.Set(packet.InputFlagStopFlying)
			position := p.Position().Add(mgl64.Vec3{0, 1.62, 0})
			h := session.PlayerAuthInputHandler{}
			if err := h.Handle(&packet.PlayerAuthInput{
				Position: mgl32.Vec3{float32(position.X()), float32(position.Y()), float32(position.Z())}, InputData: flags,
			}, p.session(), p.tx, p); err != nil {
				t.Fatal(err)
			}
			if !p.Flying() {
				t.Fatal("client disabled no-clip flight")
			}
		})
		withDamageTestPlayer(t, func(p *Player) {
			p.SetGameMode(mode)
			p.SetVelocity(mgl64.Vec3{1, -1, 1})
			pos := cube.PosFromVec3(p.Position())
			p.tx.SetBlock(pos, block.Cobweb{}, nil)
			p.checkEntityInsiders(Type.BBox(p).Translate(p.Position()))
			if got := p.Velocity(); got != (mgl64.Vec3{1, -1, 1}) {
				t.Fatalf("spectator slowed by cobweb: %v", got)
			}
			p.tx.SetBlock(pos, block.Air{}, nil)
			p.tx.SetBlock(pos.Side(cube.FaceDown), block.Slime{}, nil)
			p.fall(10)
			if p.Velocity().Y() != -1 {
				t.Fatal("spectator bounced on slime")
			}
		})
	}
}

func TestSpectatorsDoNotObstructPlacement(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(_ *world.Tx, builder, observer *Player) {
		pos := cube.PosFromVec3(observer.Position())
		if blocked, _ := builder.obstructedPos(pos, block.Stone{}); !blocked {
			t.Fatal("ordinary player did not obstruct placement fixture")
		}
		for _, mode := range []world.GameMode{world.GameModeSpectator, controlsSpectatorMode{world.GameModeSpectator}} {
			observer.SetGameMode(mode)
			if blocked, _ := builder.obstructedPos(pos, block.Stone{}); blocked {
				t.Fatal("spectator obstructed placement")
			}
		}
	})
}

func TestNativeSpectatorAbilityLayerClearsWhenReturningToSurvival(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.SetGameMode(world.GameModeSpectator)
		p.SetGameMode(world.GameModeSurvival)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			var seenSpectator bool
			var last *packet.UpdateAbilities
			for _, pk := range conn.snapshot() {
				if update, ok := pk.(*packet.UpdateAbilities); ok {
					last = update
					for _, layer := range update.AbilityData.Layers {
						if layer.Type == protocol.AbilityLayerTypeSpectator {
							seenSpectator = layer.Values&protocol.AbilityFlying != 0 && layer.Values&protocol.AbilityNoClip != 0 && layer.Values&protocol.AbilityAttackPlayers == 0
						}
					}
				}
			}
			if seenSpectator && last != nil && len(last.AbilityData.Layers) == 1 {
				if last.AbilityData.Layers[0].Values&(protocol.AbilityFlying|protocol.AbilityNoClip|protocol.AbilityInvulnerable) != 0 || p.Flying() {
					t.Fatal("survival retained spectator abilities")
				}
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("spectator/return ability updates were not sent")
	})
}
