package block_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSnowLayerStatesHaveNativeCollisionHeight(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for layers := 1; layers <= 8; layers++ {
		for _, covered := range []uint8{0, 1} {
			b, ok := world.BlockByName("minecraft:snow_layer", map[string]any{"height": int32(layers - 1), "covered_bit": covered})
			if !ok {
				t.Fatal("snow-layer state missing")
			}
			s, ok := b.(block.SnowLayer)
			if !ok || s.Layers != layers || s.Covered != (covered == 1) {
				t.Fatalf("incorrect layer state: %#v", b)
			}
			boxes := b.Model().BBox(cube.Pos{}, nil)
			if layers == 1 {
				if len(boxes) != 0 {
					t.Fatal("first snow layer must have no collision")
				}
				continue
			}
			want := float64(layers-1) / 8
			if len(boxes) != 1 || math.Abs(boxes[0].Max()[1]-want) > 1e-9 {
				t.Fatalf("%d layers: boxes %v, want height %v", layers, boxes, want)
			}
		}
	}
}
