package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// SnowLayer has a collision height one layer below its visual height: the first layer has no collision.
type SnowLayer struct{ Layers int }

func (s SnowLayer) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if s.Layers <= 1 {
		return nil
	}
	return []cube.BBox{cube.Box(0, 0, 0, 1, float64(min(s.Layers, 8)-1)/8, 1)}
}

func (s SnowLayer) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return (face == cube.FaceDown && s.Layers > 1) || (face == cube.FaceUp && s.Layers == 8)
}
