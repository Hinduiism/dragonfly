package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestBlockSnapshotCopiesLoadedState(t *testing.T) {
	w := Config{Synchronous: true, Blocks: snapshotTestRegistry()}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 4, 1}
		stone := snapshotStone{}
		tx.SetBlock(pos, stone, nil)

		snapshot, ok := tx.SnapshotBlocks(cube.Pos{0, 3, 0}, cube.Pos{2, 5, 2})
		if !ok {
			t.Fatal("expected loaded block region to be snapshotted")
		}
		if got := snapshot.Block(pos); got != stone {
			t.Fatalf("expected stone at %v, got %T", pos, got)
		}
		if _, ok := snapshot.Liquid(pos); ok {
			t.Fatal("expected no liquid at stone position")
		}
		if !snapshot.Current(tx) {
			t.Fatal("expected fresh snapshot to be current")
		}

		tx.SetBlock(cube.Pos{2, 4, 2}, stone, nil)
		if snapshot.Current(tx) {
			t.Fatal("expected block mutation to invalidate snapshot")
		}
	})
}

func TestBlockSnapshotCopiesLiquidLayer(t *testing.T) {
	w := Config{Synchronous: true, Blocks: snapshotTestRegistry()}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 4, 1}
		liquid := snapshotLiquid{}
		tx.SetLiquid(pos, liquid)

		snapshot, ok := tx.SnapshotBlocks(pos, pos)
		if !ok {
			t.Fatal("expected loaded liquid position to be snapshotted")
		}
		got, ok := snapshot.Liquid(pos)
		if !ok || got != liquid {
			t.Fatalf("expected %T liquid, got %T (found=%v)", liquid, got, ok)
		}
	})
}

func TestBlockSnapshotDoesNotLoadMissingChunks(t *testing.T) {
	w := Config{Synchronous: true, Blocks: snapshotTestRegistry()}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{32, 4, 32}
		if _, loaded := tx.World().loadedChunk(chunkPosFromBlockPos(pos)); loaded {
			t.Fatal("expected test chunk to start unloaded")
		}
		if _, ok := tx.SnapshotBlocks(pos, pos); ok {
			t.Fatal("expected snapshot of unloaded chunk to fail")
		}
		if _, loaded := tx.World().loadedChunk(chunkPosFromBlockPos(pos)); loaded {
			t.Fatal("snapshot unexpectedly loaded the missing chunk")
		}
	})
}

func TestBlockSnapshotTreatsOutOfWorldYAsAir(t *testing.T) {
	w := Config{Synchronous: true, Blocks: snapshotTestRegistry()}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{0, tx.Range().Max() + 1, 0}
		snapshot, ok := tx.SnapshotBlocks(pos, pos)
		if !ok {
			t.Fatal("expected out-of-world snapshot to succeed without chunks")
		}
		if got := snapshot.Block(pos); got != (snapshotAir{}) {
			t.Fatalf("expected air outside world bounds, got %T", got)
		}
	})
}

func TestBlockSnapshotDoesNotInitialiseMissingBlockEntity(t *testing.T) {
	w := Config{Synchronous: true, Blocks: snapshotTestRegistry()}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 4, 1}
		tx.SetBlock(pos, snapshotNBTBlock{Value: 7}, nil)
		current, ok := tx.SnapshotBlocks(pos, pos)
		if !ok {
			t.Fatal("expected valid block entity state to be snapshotted")
		}

		column, loaded := tx.World().loadedChunk(chunkPosFromBlockPos(pos))
		if !loaded {
			t.Fatal("expected test chunk to be loaded")
		}
		delete(column.BlockEntities, pos)
		if _, ok := tx.SnapshotBlocks(pos, pos); ok {
			t.Fatal("expected missing block entity state to reject the snapshot")
		}
		if _, initialised := column.BlockEntities[pos]; initialised {
			t.Fatal("snapshot unexpectedly initialised missing block entity data")
		}

		if got := tx.Block(pos); got != (snapshotNBTBlock{}) {
			t.Fatalf("expected live read to retain block entity repair behaviour, got %#v", got)
		}
		if _, initialised := column.BlockEntities[pos]; !initialised {
			t.Fatal("expected live read to initialise missing block entity data")
		}
		if current.Current(tx) {
			t.Fatal("expected live block entity initialisation to invalidate the prior snapshot")
		}
	})
}

func snapshotTestRegistry() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlock(snapshotAir{})
	registry.RegisterBlockState(BlockState{Name: "dragonfly:snapshot_stone"})
	registry.RegisterBlock(snapshotStone{})
	registry.RegisterBlockState(BlockState{Name: "dragonfly:snapshot_liquid"})
	registry.RegisterBlock(snapshotLiquid{})
	registry.RegisterBlockState(BlockState{Name: "dragonfly:snapshot_nbt"})
	registry.RegisterBlock(snapshotNBTBlock{})
	registry.Finalize()
	return registry
}

type snapshotAir struct{}

func (snapshotAir) EncodeBlock() (string, map[string]any) { return "minecraft:air", nil }
func (snapshotAir) Hash() (uint64, uint64)                { return 10001, 0 }
func (snapshotAir) Model() BlockModel                     { return snapshotEmptyModel{} }
func (snapshotAir) ReplaceableBy(Block) bool              { return true }

type snapshotStone struct{}

func (snapshotStone) EncodeBlock() (string, map[string]any) {
	return "dragonfly:snapshot_stone", nil
}
func (snapshotStone) Hash() (uint64, uint64) { return 10002, 0 }
func (snapshotStone) Model() BlockModel      { return snapshotSolidModel{} }

type snapshotLiquid struct{}

func (snapshotLiquid) EncodeBlock() (string, map[string]any) {
	return "dragonfly:snapshot_liquid", nil
}
func (snapshotLiquid) Hash() (uint64, uint64)                 { return 10003, 0 }
func (snapshotLiquid) Model() BlockModel                      { return snapshotEmptyModel{} }
func (snapshotLiquid) LiquidDepth() int                       { return 8 }
func (snapshotLiquid) SpreadDecay() int                       { return 1 }
func (snapshotLiquid) WithDepth(int, bool) Liquid             { return snapshotLiquid{} }
func (snapshotLiquid) LiquidFalling() bool                    { return false }
func (snapshotLiquid) BlastResistance() float64               { return 100 }
func (snapshotLiquid) LiquidType() string                     { return "snapshot" }
func (snapshotLiquid) Harden(cube.Pos, *Tx, *cube.Pos) bool   { return false }
func (snapshotLiquid) LiquidRemoveBlock(cube.Pos, *Tx, Block) {}

type snapshotNBTBlock struct {
	Value int
}

func (snapshotNBTBlock) EncodeBlock() (string, map[string]any) {
	return "dragonfly:snapshot_nbt", nil
}
func (snapshotNBTBlock) Hash() (uint64, uint64) { return 10004, 0 }
func (snapshotNBTBlock) Model() BlockModel      { return snapshotSolidModel{} }
func (snapshotNBTBlock) DecodeNBT(data map[string]any) any {
	value, _ := data["value"].(int)
	return snapshotNBTBlock{Value: value}
}
func (b snapshotNBTBlock) EncodeNBT() map[string]any { return map[string]any{"value": b.Value} }

type snapshotEmptyModel struct{}

func (snapshotEmptyModel) BBox(cube.Pos, BlockSource) []cube.BBox { return nil }
func (snapshotEmptyModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool {
	return false
}

type snapshotSolidModel struct{}

func (snapshotSolidModel) BBox(cube.Pos, BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
}
func (snapshotSolidModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool {
	return true
}
