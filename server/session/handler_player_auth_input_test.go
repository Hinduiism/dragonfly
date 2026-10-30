package session

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestPlayerAuthInputJumpPrecedesMovement(t *testing.T) {
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	flags.Set(packet.InputFlagStartJumping)
	flags.Set(packet.InputFlagMissedSwing)
	c := &movementInputControllable{position: mgl64.Vec3{0, 64, 0}}
	pk := &packet.PlayerAuthInput{Position: mgl32.Vec3{0, 66.04, 0}, InputData: flags}
	if err := (PlayerAuthInputHandler{}).Handle(pk, &Session{}, nil, c); err != nil {
		t.Fatal(err)
	}
	if want := []string{"jump", "swing", "move"}; !slices.Equal(c.actions, want) {
		t.Fatalf("input order = %v, want %v", c.actions, want)
	}
	if c.jumpPosition != (mgl64.Vec3{0, 64, 0}) {
		t.Fatalf("jump position = %v, want the position before movement", c.jumpPosition)
	}
	if want := (mgl64.Vec3{0, 64.42, 0}); !c.position.ApproxEqual(want) {
		t.Fatalf("final position = %v, want %v", c.position, want)
	}
}

func TestPlayerAuthInputIgnoresJumpBeforeTeleportAcknowledgement(t *testing.T) {
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	flags.Set(packet.InputFlagStartJumping)
	c := &movementInputControllable{position: mgl64.Vec3{0, 64, 0}}
	s := &Session{}
	expected := mgl64.Vec3{100, 64, 0}
	s.teleportPos.Store(&expected)
	pk := &packet.PlayerAuthInput{Position: mgl32.Vec3{0, 66.04, 0}, InputData: flags}
	if err := (PlayerAuthInputHandler{}).Handle(pk, s, nil, c); err != nil {
		t.Fatal(err)
	}
	if len(c.actions) != 0 {
		t.Fatalf("stale pre-teleport input applied actions: %v", c.actions)
	}
	if s.teleportPos.Load() == nil {
		t.Fatal("stale movement acknowledged the pending teleport")
	}
}

type movementInputControllable struct {
	Controllable
	position     mgl64.Vec3
	jumpPosition mgl64.Vec3
	actions      []string
}

func (c *movementInputControllable) Position() mgl64.Vec3  { return c.position }
func (*movementInputControllable) Rotation() cube.Rotation { return cube.Rotation{} }
func (*movementInputControllable) Sneaking() bool          { return false }
func (c *movementInputControllable) Jump() {
	c.actions = append(c.actions, "jump")
	c.jumpPosition = c.position
}
func (c *movementInputControllable) PunchAir() { c.actions = append(c.actions, "swing") }
func (c *movementInputControllable) Move(delta mgl64.Vec3, _, _ float64) {
	c.actions = append(c.actions, "move")
	c.position = c.position.Add(delta)
}

func TestPlayerAuthInputFeetPositionSubtractsInFloat64(t *testing.T) {
	got := playerAuthInputFeetPosition(mgl32.Vec3{2, 1.00325, -3})
	want := mgl64.Vec3{2, -0.6167, -3}
	if !got.ApproxEqualThreshold(want, 1e-12) {
		t.Fatalf("feet position: got %v, want %v", got, want)
	}
}

func TestRoundPlayerAuthInputPosition(t *testing.T) {
	tests := []struct {
		name string
		in   mgl64.Vec3
		want mgl64.Vec3
	}{
		{name: "positive", in: mgl64.Vec3{1.23456, 72.00004, 9.87654}, want: mgl64.Vec3{1.2346, 72, 9.8765}},
		{name: "negative", in: mgl64.Vec3{-1.23456, -0.00006, -9.87654}, want: mgl64.Vec3{-1.2346, -0.0001, -9.8765}},
		{name: "half away from zero", in: mgl64.Vec3{1.23445, -1.23445, 0}, want: mgl64.Vec3{1.2345, -1.2345, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundPlayerAuthInputPosition(tt.in)
			if !got.ApproxEqualThreshold(tt.want, 1e-12) {
				t.Fatalf("round position: got %v, want %v", got, tt.want)
			}
		})
	}
}
