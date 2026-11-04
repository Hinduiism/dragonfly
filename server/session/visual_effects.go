package session

import (
	"image/color"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ViewParticleInTx preserves the owner transaction for visual particles whose
// temporary actors need session-owned IDs and bounded world-bound scheduling.
// Ordinary particles keep the existing encoder and Viewer compatibility.
func (s *Session) ViewParticleInTx(tx *world.Tx, pos mgl64.Vec3, p world.Particle) {
	var identifier string
	var lifetime time.Duration
	var rocket bool
	metadata := protocol.NewEntityMetadata()
	metadata.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSilent)
	switch value := p.(type) {
	case particle.Lightning:
		identifier, lifetime = "minecraft:lightning_bolt", 350*time.Millisecond
	case particle.Firework:
		identifier, lifetime, rocket = "minecraft:fireworks_rocket", value.Duration, true
		if lifetime == 0 {
			lifetime = 600 * time.Millisecond
		}
		lifetime = min(2*time.Second, max(200*time.Millisecond, lifetime))
		firework := item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{{
			Shape: item.FireworkShapeSmallSphere(), Colour: closestFireworkColour(value.Colour),
			Fade: item.ColourYellow(), Fades: true, Trail: true,
		}}}
		metadata[protocol.EntityDataKeyDisplayFirework] = item.WriteNBT(item.NewStack(firework, 1), false)
	default:
		s.ViewParticle(pos, p)
		return
	}
	if !s.entityViewOwner(tx) {
		return
	}
	v, err := s.addEntityView(tx, identifier, EntityViewState{Position: pos}, metadata)
	if err != nil {
		return
	}
	s.scheduleVisualFrame(v, pos, time.Now(), lifetime, rocket)
}

func closestFireworkColour(value color.RGBA) item.Colour {
	if value == (color.RGBA{}) {
		return item.ColourMagenta()
	}
	best, distance := item.ColourWhite(), math.MaxFloat64
	for _, candidate := range item.Colours() {
		c := candidate.RGBA()
		r, g, b := float64(c.R)-float64(value.R), float64(c.G)-float64(value.G), float64(c.B)-float64(value.B)
		if d := r*r + g*g + b*b; d < distance {
			best, distance = candidate, d
		}
	}
	return best
}

func (s *Session) scheduleVisualFrame(v *EntityView, origin mgl64.Vec3, started time.Time, lifetime time.Duration, rocket bool) {
	v.world.DoAfter(50*time.Millisecond, func(tx *world.Tx) {
		if s.stepVisualFrame(tx, v, origin, time.Since(started), lifetime, rocket) {
			s.scheduleVisualFrame(v, origin, started, lifetime, rocket)
		}
	})
}

func (s *Session) stepVisualFrame(tx *world.Tx, v *EntityView, origin mgl64.Vec3, elapsed, lifetime time.Duration, rocket bool) bool {
	if v.Closed() || !s.entityViewOwner(tx) || tx.World() != v.world {
		return false
	}
	if rocket {
		position := origin.Add(mgl64.Vec3{0, 1.25 * min(1, float64(elapsed)/float64(lifetime)), 0})
		v.state.Position, v.sent.Position = position, position
		s.writePacket(&packet.MoveActorAbsolute{EntityRuntimeID: v.id, Position: vec64To32(position)})
	}
	if elapsed >= lifetime {
		if rocket {
			s.writePacket(&packet.ActorEvent{EntityRuntimeID: v.id, EventType: packet.ActorEventFireworksExplode})
		}
		s.removeEntityView(v)
		return false
	}
	return true
}
