package world

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSynchronousWorldDo verifies that Do on a synchronous World runs the task
// on the calling goroutine and returns a completed task.
func TestSynchronousWorldDo(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	var ran bool
	task := w.Do(func(tx *Tx) { ran = true })
	if !ran {
		t.Fatal("expected task to have run when Do returned")
	}
	select {
	case <-task.Done():
	default:
		t.Fatal("expected task returned by Do to be done when Do returned")
	}
}

// TestSynchronousWorldAdvanceTick verifies that a synchronous World does not
// tick on its own and that AdvanceTick advances the current tick exactly once
// per call, even without any viewers.
func TestSynchronousWorldAdvanceTick(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	current := func() int64 {
		w.set.Lock()
		defer w.set.Unlock()
		return w.set.CurrentTick
	}
	start := current()
	time.Sleep(time.Second / 10)
	if got := current(); got != start {
		t.Fatalf("expected no automatic ticking, tick advanced from %v to %v", start, got)
	}
	for range 5 {
		w.AdvanceTick()
	}
	if got := current(); got != start+5 {
		t.Fatalf("expected current tick %v after 5 AdvanceTick calls, got %v", start+5, got)
	}
}

func TestSynchronousEntityDoCanRemoveEntity(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	h := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	<-w.exec(func(tx *Tx) {
		tx.AddEntity(h)
	})

	task := h.Do(func(tx *Tx, e Entity) {
		tx.RemoveEntity(e)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if err := task.Wait(ctx); err != nil {
		t.Fatalf("entity Do self-removal did not complete: %v", err)
	}
}

func TestSynchronousEntityDoWaitsForAddEntityToFinish(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	state := &blockingOpenState{
		firstOpen:  make(chan struct{}),
		secondOpen: make(chan struct{}),
		release:    make(chan struct{}),
	}
	h := EntitySpawnOpts{}.New(blockingOpenType{}, blockingOpenConfig{state: state})
	task := h.Do(func(*Tx, Entity) {})
	added := make(chan struct{})
	go func() {
		w.Do(func(tx *Tx) { tx.AddEntity(h) })
		close(added)
	}()
	<-state.firstOpen

	premature := false
	select {
	case <-state.secondOpen:
		premature = true
	case <-time.After(time.Millisecond * 50):
	}
	close(state.release)
	<-added
	if err := task.Wait(context.Background()); err != nil {
		t.Fatalf("entity Do failed: %v", err)
	}
	if premature {
		t.Fatal("entity callback opened before AddEntity completed")
	}
}

func TestSynchronousAdvanceTickTicksViewerlessEntities(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	h := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	<-w.exec(func(tx *Tx) {
		tx.AddEntity(h)
	})

	start := h.data.Pos
	for range 3 {
		w.AdvanceTick()
	}
	if got := h.data.Pos; got == start {
		t.Fatalf("expected entity position to change after ticking, got %v", got)
	}
}

func TestSynchronousAdvanceTickTicksViewerlessBlockEntities(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	pos := cube.Pos{0, 4, 0}
	tb := &testTickerBlock{}
	<-w.exec(func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		chest, ok := tx.World().conf.Blocks.BlockByName("minecraft:chest", map[string]any{"minecraft:cardinal_direction": "north"})
		if !ok {
			t.Fatal("expected chest block to be registered")
		}
		col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0, tx.World().conf.Blocks.BlockRuntimeID(chest))
		col.BlockEntities[pos] = tb
	})

	w.AdvanceTick()
	if tb.ticks == 0 {
		t.Fatal("expected block entity to tick")
	}
}

func TestUnloadChunkIfUnusedAbsent(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		if tx.UnloadChunkIfUnused(ChunkPos{4, -7}) {
			t.Fatal("expected absent chunk not to unload")
		}
	})
}

func TestUnloadChunkIfUnusedViewed(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	pos := ChunkPos{2, 3}
	loader := NewLoader(0, w, NopViewer{})
	w.Do(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{float64(pos[0] << 4), 0, float64(pos[1] << 4)})
		loader.Load(tx, 1)
		if _, ok := loader.Chunk(pos); !ok {
			t.Fatal("expected loader to own candidate chunk")
		}
		if tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected viewed chunk not to unload")
		}
		if _, ok := w.loadedChunk(pos); !ok {
			t.Fatal("expected viewed chunk to remain loaded")
		}

		loader.Close(tx)
		if !tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected released chunk to unload")
		}
	})
}

func TestUnloadChunkIfUnusedModified(t *testing.T) {
	provider := &countingStoreProvider{}
	w := Config{Provider: provider, Synchronous: true}.New()
	defer w.Close()

	pos := ChunkPos{-3, 9}
	w.Do(func(tx *Tx) {
		col := tx.chunk(pos)
		col.modified = true
		if !tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected unused modified chunk to unload")
		}
		if _, ok := w.loadedChunk(pos); ok {
			t.Fatal("expected unloaded chunk to be evicted")
		}
		if tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected repeated unload to report absent chunk")
		}
	})
	if got := provider.stores.Load(); got != 1 {
		t.Fatalf("expected one provider store, got %d", got)
	}
}

func TestUnloadChunkIfUnusedUnmodified(t *testing.T) {
	provider := &countingStoreProvider{}
	w := Config{Provider: provider, Synchronous: true}.New()
	defer w.Close()

	pos := ChunkPos{8, 1}
	w.Do(func(tx *Tx) {
		tx.chunk(pos)
		if !tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected unused unmodified chunk to unload")
		}
	})
	if got := provider.stores.Load(); got != 0 {
		t.Fatalf("expected no provider store, got %d", got)
	}
}

type countingStoreProvider struct {
	NopProvider
	stores atomic.Int64
}

func (p *countingStoreProvider) StoreColumn(ChunkPos, Dimension, *chunk.Column) error {
	p.stores.Add(1)
	return nil
}

type testEntityConfig struct{}

func (testEntityConfig) Apply(*EntityData) {}

type testEntityType struct{}

func (testEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &testEntity{handle: handle, data: data}
}

func (testEntityType) EncodeEntity() string {
	return "dragonfly:test_entity"
}

func (testEntityType) BBox(Entity) cube.BBox {
	return cube.Box(0, 0, 0, 1, 1, 1)
}

func (testEntityType) DecodeNBT(map[string]any, *EntityData) {}

func (testEntityType) EncodeNBT(*EntityData) map[string]any {
	return nil
}

type testEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e *testEntity) Close() error {
	return nil
}

func (e *testEntity) H() *EntityHandle {
	return e.handle
}

func (e *testEntity) Position() mgl64.Vec3 {
	return e.data.Pos
}

func (e *testEntity) Rotation() cube.Rotation {
	return e.data.Rot
}

func (e *testEntity) Tick(*Tx, int64) {
	e.data.Pos = e.data.Pos.Add(mgl64.Vec3{0, -0.1, 0})
}

type testTickerBlock struct {
	ticks int
}

type blockingOpenState struct {
	opens      atomic.Int32
	firstOpen  chan struct{}
	secondOpen chan struct{}
	release    chan struct{}
}

type blockingOpenConfig struct {
	state *blockingOpenState
}

func (c blockingOpenConfig) Apply(data *EntityData) { data.Data = c.state }

type blockingOpenType struct{}

func (blockingOpenType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	state := data.Data.(*blockingOpenState)
	switch state.opens.Add(1) {
	case 1:
		close(state.firstOpen)
		<-state.release
	case 2:
		close(state.secondOpen)
	}
	return &testEntity{handle: handle, data: data}
}

func (blockingOpenType) EncodeEntity() string { return "dragonfly:blocking_open" }

func (blockingOpenType) BBox(Entity) cube.BBox { return cube.BBox{} }

func (blockingOpenType) DecodeNBT(map[string]any, *EntityData) {}

func (blockingOpenType) EncodeNBT(*EntityData) map[string]any { return nil }

func (*testTickerBlock) EncodeBlock() (string, map[string]any) {
	return "dragonfly:test_ticker", nil
}

func (*testTickerBlock) Hash() (uint64, uint64) {
	return 1<<32 - 1, 0
}

func (*testTickerBlock) Model() BlockModel {
	return unknownModel{}
}

func (*testTickerBlock) DecodeNBT(map[string]any) any {
	return &testTickerBlock{}
}

func (*testTickerBlock) EncodeNBT() map[string]any {
	return nil
}

func (b *testTickerBlock) Tick(int64, cube.Pos, *Tx) {
	b.ticks++
}
