package world

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type lightLevelsProvider struct {
	NopProvider
	loads atomic.Int64
}

func (p *lightLevelsProvider) LoadColumn(pos ChunkPos, dim Dimension) (*chunk.Column, error) {
	p.loads.Add(1)
	return p.NopProvider.LoadColumn(pos, dim)
}

func TestLightLevelsDoesNotLoadChunks(t *testing.T) {
	provider := &lightLevelsProvider{}
	w := Config{Provider: provider, Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{32, 64, 32}
		sky, block := tx.LightLevels(pos)
		if sky != 0 || block != 0 {
			t.Fatalf("expected no light for an unloaded chunk, got sky=%v block=%v", sky, block)
		}
		if loads := provider.loads.Load(); loads != 0 {
			t.Fatalf("LightLevels loaded %v columns", loads)
		}

		above := cube.Pos{0, tx.Range()[1] + 1, 0}
		sky, block = tx.LightLevels(above)
		if sky != 15 || block != 0 {
			t.Fatalf("expected full sky light above the world, got sky=%v block=%v", sky, block)
		}
		if loads := provider.loads.Load(); loads != 0 {
			t.Fatalf("above-range LightLevels loaded %v columns", loads)
		}
	})
}

func TestLightLevelsReturnsIndependentChannels(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 64, 1}
		column := tx.chunk(chunkPosFromBlockPos(pos))
		sub := column.SubChunk(int16(pos.Y()))
		sub.SetSkyLight(uint8(pos.X()&15), uint8(pos.Y()&15), uint8(pos.Z()&15), 13)
		sub.SetBlockLight(uint8(pos.X()&15), uint8(pos.Y()&15), uint8(pos.Z()&15), 7)

		sky, block := tx.LightLevels(pos)
		if sky != 13 || block != 7 {
			t.Fatalf("expected sky=13 and block=7, got sky=%v block=%v", sky, block)
		}
	})
}
