package session

import (
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"image/color"
	"testing"
)

func TestTextSubmissionSnapshotsFramesAndKeepsIdentity(t *testing.T) {
	s := newEnvironmentViewTestSession()
	s.debugShapes = make(map[int]debug.Shape)
	text := &debug.Text{Position: mgl64.Vec3{1, 2, 3}, Text: "glyph", Colour: color.RGBA{255, 255, 255, 255}}
	id := text.ShapeID()
	s.AddDebugShape(text)
	text.Position[1] = 20
	text.Colour.A = 10
	s.SendDebugShapes(world.Overworld)
	first := packetOf[*packet.PrimitiveShapes](t, s).Shapes[0]
	position, _ := first.Location.Value()
	colour, _ := first.Colour.Value()
	if first.NetworkID != uint64(id) || position[1] != 2 || colour.A != 255 {
		t.Fatal("queued frame mutated", first)
	}
	s.AddDebugShape(text)
	s.SendDebugShapes(world.Overworld)
	next := packetOf[*packet.PrimitiveShapes](t, s).Shapes[0]
	position, _ = next.Location.Value()
	colour, _ = next.Colour.Value()
	if next.NetworkID != first.NetworkID || position[1] != 20 || colour.A != 10 {
		t.Fatal("frame update lost identity", next)
	}
	s.RemoveDebugShape(text)
	s.SendDebugShapes(world.Overworld)
	if len(s.debugShapes) != 0 {
		t.Fatal("text removal failed")
	}
}
