package block

import (
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
)

// SnowLayer is one to eight visual layers of snow. Covered records the native covered_bit state.
type SnowLayer struct {
	transparent
	Layers  int
	Covered bool
}

func (s SnowLayer) Model() world.BlockModel { return model.SnowLayer{Layers: s.Layers} }

func (s SnowLayer) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, shovelEffective, oneOf(SnowLayer{Layers: 1}))
}

func (SnowLayer) EncodeItem() (string, int16) { return "minecraft:snow_layer", 0 }

func (s SnowLayer) EncodeBlock() (string, map[string]any) {
	return "minecraft:snow_layer", map[string]any{"height": int32(s.Layers - 1), "covered_bit": boolByte(s.Covered)}
}

func allSnowLayers() (blocks []world.Block) {
	for layers := 1; layers <= 8; layers++ {
		for _, covered := range []bool{false, true} {
			blocks = append(blocks, SnowLayer{Layers: layers, Covered: covered})
		}
	}
	return
}
