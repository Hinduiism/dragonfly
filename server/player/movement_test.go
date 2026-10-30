package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestStationaryMovementPreservesGroundState(t *testing.T) {
	for _, grounded := range []bool{false, true} {
		name := "airborne"
		if grounded {
			name = "grounded"
		}
		t.Run(name, func(t *testing.T) {
			withStateTestPlayer(t, func(p *Player, _ *stateConn) {
				p.data.Pos = mgl64.Vec3{0.5, 64, 0.5}
				if grounded {
					p.tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
				}
				// Establish the ground state through the normal movement path.
				p.Move(mgl64.Vec3{0.1, 0, 0}, 0, 0)
				if p.OnGround() != grounded {
					t.Fatalf("initial on-ground state = %v, want %v", p.OnGround(), grounded)
				}
				if !grounded {
					p.fallDistance = 3
				}
				fallDistance, velocity := p.fallDistance, p.Velocity()
				p.Move(mgl64.Vec3{}, 0, 0)
				if p.OnGround() != grounded {
					t.Fatalf("stationary movement changed on-ground state to %v", p.OnGround())
				}
				if p.fallDistance != fallDistance {
					t.Fatalf("stationary movement changed fall distance from %v to %v", fallDistance, p.fallDistance)
				}
				p.Jump()
				want := velocity
				if grounded {
					want = mgl64.Vec3{0, 0.42, 0}
				}
				if got := p.Velocity(); !got.ApproxEqual(want) {
					t.Fatalf("jump velocity = %v, want %v", got, want)
				}
			})
		})
	}
}
