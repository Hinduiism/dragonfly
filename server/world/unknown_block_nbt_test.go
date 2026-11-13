package world

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func unknownNBTTestWorld() (*World, Block) {
	br := NewBlockRegistry()
	br.RegisterBlock(snapshotAir{})
	br.RegisterBlockState(BlockState{Name: "dragonfly:unknown_terrain"})
	br.RegisterBlockState(BlockState{Name: "dragonfly:snapshot_nbt"})
	br.RegisterBlock(snapshotNBTBlock{})
	w := Config{Synchronous: true, Blocks: br}.New()
	b, _ := br.BlockByName("dragonfly:unknown_terrain", nil)
	return w, b
}

func TestUnknownBlockReadDoesNotCreateActor(t *testing.T) {
	w, b := unknownNBTTestWorld()
	defer w.Close()
	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 4, 1}
		col := tx.chunk(ChunkPos{})
		col.SetBlock(1, 4, 1, 0, w.BlockRegistry().BlockRuntimeID(b))
		v := &unknownNBTViewer{}
		col.viewers = append(col.viewers, v)
		defer func() { col.viewers = col.viewers[:len(col.viewers)-1] }()
		revision := tx.blockRevision
		for range 2 {
			if got := tx.Block(pos); !IsUnimplementedBlock(got) {
				t.Fatalf("expected unsupported terrain, got %T", got)
			}
		}
		if len(col.BlockEntities) != 0 || v.updates != 0 || tx.blockRevision != revision {
			t.Fatalf("read manufactured actor state: actors=%d updates=%d revision=%d->%d", len(col.BlockEntities), v.updates, revision, tx.blockRevision)
		}
		snapshot, ok := tx.SnapshotBlocks(pos, pos)
		if !ok || !IsUnimplementedBlock(snapshot.Block(pos)) {
			t.Fatal("ordinary unknown terrain must remain available to snapshots")
		}
	})
}

func TestUnknownBlockWritesOnlyStoreMeaningfulActorData(t *testing.T) {
	w, b := unknownNBTTestWorld()
	defer w.Close()
	withData := b.(NBTer).DecodeNBT(map[string]any{"id": "UnsupportedTile", "value": int32(7)}).(Block)
	w.Do(func(tx *Tx) {
		pos := cube.Pos{1, 4, 1}
		for name, write := range map[string]func(Block){
			"SetBlock":       func(b Block) { tx.SetBlock(pos, b, nil) },
			"SetBlocks":      func(b Block) { tx.SetBlocks([]BlockChange{{Position: pos, Block: b}}, nil) },
			"SetBlockEntity": func(b Block) { tx.SetBlockEntity(pos, b) },
			"BuildStructure": func(b Block) { tx.BuildStructure(pos, unknownNBTStructure{b}) },
		} {
			write(withData)
			col := tx.chunk(ChunkPos{})
			if got := col.BlockEntities[pos]; !reflect.DeepEqual(got, withData) {
				t.Fatalf("%s lost genuine unsupported actor: %#v", name, got)
			}
			write(b)
			if _, ok := col.BlockEntities[pos]; ok {
				t.Fatalf("%s retained empty unsupported actor", name)
			}
		}
	})
}

func TestUnknownBlockLoadAndSaveRemovesOnlyEmptyActors(t *testing.T) {
	w, b := unknownNBTTestWorld()
	defer w.Close()
	data := []map[string]any{
		nil,
		{},
		{"x": int32(2), "y": int32(4), "z": int32(0)},
		{"id": "UnsupportedTile"},
		{"CustomPayload": []byte{1, 2, 3}}, // Preserve meaningful data even without an id.
	}
	c := &chunk.Column{Chunk: chunk.New(w.BlockRegistry(), w.Range())}
	for i, d := range data {
		pos := cube.Pos{i, 4, 0}
		c.Chunk.SetBlock(uint8(i), 4, 0, 0, w.BlockRegistry().BlockRuntimeID(b))
		c.BlockEntities = append(c.BlockEntities, chunk.BlockEntity{Pos: pos, Data: d})
	}
	col := w.columnFrom(c, ChunkPos{})
	if !col.modified || len(col.BlockEntities) != 2 {
		t.Fatalf("expected cleaned column with two real actors, modified=%v actors=%d", col.modified, len(col.BlockEntities))
	}
	// A stale entry inserted by a caller must also be safe to serialise.
	col.BlockEntities[cube.Pos{0, 4, 0}] = b
	saved := w.columnTo(col, ChunkPos{})
	if len(saved.BlockEntities) != 2 {
		t.Fatalf("saved %d actors, expected two", len(saved.BlockEntities))
	}
	for _, actor := range saved.BlockEntities {
		if !reflect.DeepEqual(actor.Data, data[actor.Pos.X()]) {
			t.Fatalf("changed genuine actor data: %#v", actor)
		}
	}
}

func TestUnknownBlockEncodeNBTOmitsCoordinatesOnly(t *testing.T) {
	for _, data := range []map[string]any{nil, {}, {"x": int32(1), "y": int32(4), "z": int32(1)}} {
		b := unknownBlock{data: data}
		if got := b.EncodeNBT(); got != nil {
			t.Fatalf("empty actor encoded as %#v", got)
		}
	}
}

type unknownNBTViewer struct {
	NopViewer
	updates int
}

func (v *unknownNBTViewer) ViewBlockUpdate(cube.Pos, Block, int) { v.updates++ }

type unknownNBTStructure struct{ b Block }

func (unknownNBTStructure) Dimensions() [3]int { return [3]int{1, 1, 1} }
func (s unknownNBTStructure) At(int, int, int, func(int, int, int) Block) (Block, Liquid) {
	return s.b, nil
}
