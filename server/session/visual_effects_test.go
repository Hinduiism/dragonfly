package session

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestVisualActorsStayPrivateSilentAndOutsideWorld(t *testing.T) {
	for _, effect := range []world.Particle{particle.Lightning{}, particle.Firework{}} {
		t.Run(effectName(effect), func(t *testing.T) {
			s, w := newEntityViewFixture(t)
			other := newEnvironmentViewTestSession()
			if err := w.Do(func(tx *world.Tx) {
				s.ViewParticleInTx(tx, mgl64.Vec3{0, 64, 0}, effect)
				spawn := packetOf[*packet.AddActor](t, s)
				if !spawn.EntityMetadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSilent) ||
					spawn.EntityMetadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasCollision) {
					t.Fatal("decorative actor was not silent/non-colliding")
				}
				if len(s.entityViews) != 1 || len(s.entities) != 0 || len(other.packets) != 0 {
					t.Fatal("visual leaked into gameplay or another session")
				}
				count := 0
				for range tx.Entities() {
					count++
				}
				if count != 1 {
					t.Fatal("visual added a world entity")
				}
				if _, ok := effect.(particle.Firework); ok {
					if spawn.EntityMetadata[protocol.EntityDataKeyDisplayFirework] == nil {
						t.Fatal("firework metadata missing")
					}
				}
				for _, v := range s.entityViews {
					rocket := spawn.EntityType == entity.FireworkType.EncodeEntity()
					if s.stepVisualFrame(tx, v, v.state.Position, time.Second, 600*time.Millisecond, rocket) {
						t.Fatal("visual did not finish")
					}
					if rocket {
						packetOf[*packet.MoveActorAbsolute](t, s)
						packetOf[*packet.ActorEvent](t, s)
					}
					packetOf[*packet.RemoveActor](t, s)
				}
				if len(s.entityViews) != 0 || len(s.packets) != 0 {
					t.Fatal("visual retained state or added audio")
				}
			}).Wait(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func effectName(p world.Particle) string {
	if _, ok := p.(particle.Lightning); ok {
		return "lightning"
	}
	return "firework"
}
