package world

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTickPolicyZeroEnablesAllSubsystems(t *testing.T) {
	var policy TickPolicy
	for subsystem := TickSubsystem(1); subsystem <= TickNonPlayerEntities; subsystem <<= 1 {
		if !policy.Enabled(subsystem) {
			t.Fatalf("zero policy disabled subsystem %#x", subsystem)
		}
	}
	if got := TickAllSubsystems; got != TickSubsystem(0xff) {
		t.Fatalf("TickAllSubsystems = %#x, want %#x", got, TickSubsystem(0xff))
	}
}

func TestDisabledTickWorkDoesNotAccumulate(t *testing.T) {
	w := Config{
		Synchronous: true,
		TickPolicy:  TickPolicy{Disabled: TickScheduledBlocks | TickNeighbourUpdates | TickRedstone | TickSleep},
	}.New()
	defer w.Close()

	pos := cube.Pos{0, 4, 0}
	w.Do(func(tx *Tx) {
		stone, ok := tx.World().conf.Blocks.BlockByName("minecraft:stone", nil)
		if !ok {
			t.Fatal("stone block is not registered")
		}
		tx.SetBlock(pos, stone, nil)
		tx.ScheduleBlockUpdate(pos, stone, time.Second)
		tx.Redstone().ScheduleUpdate(pos)
		if len(w.neighbourUpdates) != 0 {
			t.Fatalf("disabled neighbour updates queued %d entries", len(w.neighbourUpdates))
		}
		if len(w.scheduledUpdates.ticks) != 0 || len(w.scheduledUpdates.furthestTicks) != 0 {
			t.Fatal("disabled scheduled updates accumulated work")
		}
		if len(w.redstone.dirty) != 0 {
			t.Fatal("disabled redstone accumulated dirty positions")
		}
		burnedOut, recoverable := tx.Redstone().Torch(pos).BurnoutStatus()
		if burnedOut || !recoverable {
			t.Fatalf("disabled redstone burnout status = %v, %v; want false, true", burnedOut, recoverable)
		}
		if power := tx.RedstonePower(pos); power != 0 {
			t.Fatalf("disabled redstone power = %d, want 0", power)
		}
	})

	w.set.Lock()
	w.set.RequiredSleepTicks = 5
	startTick := w.set.CurrentTick
	startTime := w.set.Time
	w.set.Unlock()
	w.AdvanceTick()
	w.set.Lock()
	defer w.set.Unlock()
	if w.set.RequiredSleepTicks != 5 {
		t.Fatalf("disabled sleep countdown = %d, want 5", w.set.RequiredSleepTicks)
	}
	if w.set.CurrentTick != startTick+1 {
		t.Fatalf("current tick = %d, want %d", w.set.CurrentTick, startTick+1)
	}
	if w.set.Time != startTime+1 {
		t.Fatalf("visible time = %d, want %d", w.set.Time, startTime+1)
	}
}

func TestDisabledRandomTicksDoNotConsumeRandomSource(t *testing.T) {
	source := &countingRandSource{}
	w := Config{
		Synchronous: true,
		RandSource:  source,
		TickPolicy: TickPolicy{
			Disabled: TickRandomBlocks | TickBlockEntities | TickLightning,
		},
	}.New()
	defer w.Close()
	w.Do(func(tx *Tx) {
		stone, ok := tx.World().conf.Blocks.BlockByName("minecraft:stone", nil)
		if !ok {
			t.Fatal("stone block is not registered")
		}
		tx.SetBlock(cube.Pos{0, 4, 0}, stone, &SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	})
	before := source.calls
	w.AdvanceTick()
	if source.calls != before {
		t.Fatalf("disabled random ticking consumed %d random values", source.calls-before)
	}
}

func TestDisabledLightningSkipsStrikeAttempts(t *testing.T) {
	source := &countingRandSource{}
	w := Config{
		Synchronous: true,
		RandSource:  source,
		TickPolicy: TickPolicy{
			Disabled: TickRandomBlocks | TickBlockEntities | TickLightning,
		},
	}.New()
	defer w.Close()
	w.Do(func(tx *Tx) {
		tx.chunk(ChunkPos{})
	})
	w.set.Lock()
	w.set.Raining = true
	w.set.Thundering = true
	w.set.WeatherCycle = false
	w.set.Unlock()
	before := source.calls
	w.AdvanceTick()
	if source.calls != before {
		t.Fatalf("disabled lightning consumed %d random values", source.calls-before)
	}
}

func TestRandomAndBlockEntityTickPoliciesAreIndependent(t *testing.T) {
	w := Config{Synchronous: true, TickPolicy: TickPolicy{Disabled: TickRandomBlocks}}.New()
	defer w.Close()

	pos := cube.Pos{0, 4, 0}
	blockEntity := &testTickerBlock{}
	w.Do(func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		chest, ok := tx.World().conf.Blocks.BlockByName("minecraft:chest", map[string]any{"minecraft:cardinal_direction": "north"})
		if !ok {
			t.Fatal("chest block is not registered")
		}
		col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0, tx.World().conf.Blocks.BlockRuntimeID(chest))
		col.BlockEntities[pos] = blockEntity
	})
	w.AdvanceTick()
	if blockEntity.ticks != 1 {
		t.Fatalf("block entity ticks = %d, want 1", blockEntity.ticks)
	}
}

func TestDisabledBlockEntityTicksAreSkipped(t *testing.T) {
	w := Config{Synchronous: true, TickPolicy: TickPolicy{Disabled: TickBlockEntities}}.New()
	defer w.Close()

	pos := cube.Pos{0, 4, 0}
	blockEntity := &testTickerBlock{}
	w.Do(func(tx *Tx) {
		col := tx.chunk(chunkPosFromBlockPos(pos))
		chest, ok := tx.World().conf.Blocks.BlockByName("minecraft:chest", map[string]any{"minecraft:cardinal_direction": "north"})
		if !ok {
			t.Fatal("chest block is not registered")
		}
		col.SetBlock(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0, tx.World().conf.Blocks.BlockRuntimeID(chest))
		col.BlockEntities[pos] = blockEntity
	})
	w.AdvanceTick()
	if blockEntity.ticks != 0 {
		t.Fatalf("disabled block entity ticked %d time(s)", blockEntity.ticks)
	}
}

func TestNonPlayerEntityPolicyStillTicksPlayers(t *testing.T) {
	w := Config{Synchronous: true, TickPolicy: TickPolicy{Disabled: TickNonPlayerEntities}}.New()
	defer w.Close()

	nonPlayer := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	player := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(tickPolicyPlayerType{}, testEntityConfig{})
	w.Do(func(tx *Tx) {
		tx.AddEntity(nonPlayer)
		tx.AddEntity(player)
	})
	nonPlayerStart, playerStart := nonPlayer.data.Pos, player.data.Pos
	w.AdvanceTick()
	if nonPlayer.data.Pos != nonPlayerStart {
		t.Fatalf("disabled non-player moved from %v to %v", nonPlayerStart, nonPlayer.data.Pos)
	}
	if player.data.Pos == playerStart {
		t.Fatalf("player did not tick at %v", playerStart)
	}
}

func TestEntityStorageModeControlsColumnEntities(t *testing.T) {
	registry := EntityRegistryConfig{}.New([]EntityType{testEntityType{}})
	stored := chunk.Entity{ID: 7, Data: map[string]any{
		"identifier": "dragonfly:test_entity",
		"Pos":        []float32{0, 4, 0},
		"Motion":     []float32{0, 0, 0},
		"Yaw":        float32(0),
		"Pitch":      float32(0),
	}}

	for _, test := range []struct {
		name string
		mode EntityStorageMode
		want int
	}{
		{name: "persistent", mode: EntityStoragePersistent, want: 1},
		{name: "transient", mode: EntityStorageTransient, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := Config{Synchronous: true, Entities: registry, EntityStorage: test.mode}.New()
			defer w.Close()

			disk := &chunk.Column{
				Chunk:    chunk.New(w.conf.Blocks, w.Range()),
				Entities: []chunk.Entity{stored},
			}
			loaded := w.columnFrom(disk, ChunkPos{})
			if got := len(loaded.Entities); got != test.want {
				t.Fatalf("loaded entities = %d, want %d", got, test.want)
			}

			handle := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
			loaded.Entities = []*EntityHandle{handle}
			encoded := w.columnTo(loaded, ChunkPos{})
			if got := len(encoded.Entities); got != test.want {
				t.Fatalf("stored entities = %d, want %d", got, test.want)
			}
		})
	}
}

func TestTransientEntityStorageDoesNotAffectRuntimeEntities(t *testing.T) {
	w := Config{Synchronous: true, EntityStorage: EntityStorageTransient}.New()
	defer w.Close()
	handle := EntitySpawnOpts{Position: mgl64.Vec3{0, 4, 0}}.New(testEntityType{}, testEntityConfig{})
	w.Do(func(tx *Tx) {
		tx.AddEntity(handle)
	})
	start := handle.data.Pos
	w.AdvanceTick()
	if handle.data.Pos == start {
		t.Fatal("transient runtime entity did not tick")
	}
}

func TestDisabledScheduledTicksAreNotLoadedOrStored(t *testing.T) {
	w := Config{Synchronous: true, TickPolicy: TickPolicy{Disabled: TickScheduledBlocks}}.New()
	defer w.Close()
	stone, ok := w.conf.Blocks.BlockByName("minecraft:stone", nil)
	if !ok {
		t.Fatal("stone block is not registered")
	}
	disk := &chunk.Column{
		Chunk: chunk.New(w.conf.Blocks, w.Range()),
		ScheduledBlocks: []chunk.ScheduledBlockUpdate{{
			Pos:   cube.Pos{0, 4, 0},
			Block: w.conf.Blocks.BlockRuntimeID(stone),
			Tick:  20,
		}},
	}
	loaded := w.columnFrom(disk, ChunkPos{})
	if len(w.scheduledUpdates.ticks) != 0 {
		t.Fatal("disabled scheduled ticks were loaded")
	}
	if stored := w.columnTo(loaded, ChunkPos{}); len(stored.ScheduledBlocks) != 0 {
		t.Fatal("disabled scheduled ticks were stored")
	}
}

func TestWorldMaxChunkRadiusNormalizesNegativeValues(t *testing.T) {
	uncapped := Config{Synchronous: true, MaxChunkRadius: -5}.New()
	defer uncapped.Close()
	if got := uncapped.MaxChunkRadius(); got != 0 {
		t.Fatalf("negative cap normalized to %d, want 0", got)
	}

	capped := Config{Synchronous: true, MaxChunkRadius: 6}.New()
	defer capped.Close()
	if got := capped.MaxChunkRadius(); got != 6 {
		t.Fatalf("cap = %d, want 6", got)
	}
}

func TestLoaderChangesWorldAndRadiusTogether(t *testing.T) {
	first := Config{Synchronous: true}.New()
	second := Config{Synchronous: true}.New()
	defer first.Close()
	defer second.Close()

	loader := NewLoader(5, first, NopViewer{})
	loader.ChangeWorldAndRadius(nil, second, 2)
	if loader.World() != second {
		t.Fatal("loader did not switch worlds")
	}
	loader.mu.RLock()
	defer loader.mu.RUnlock()
	if loader.r != 2 {
		t.Fatalf("loader radius = %d, want 2", loader.r)
	}
	for _, pos := range loader.loadQueue {
		if !loader.withinLoadRadius(pos) {
			t.Fatalf("queued chunk %v is outside the new radius", pos)
		}
	}
}

type tickPolicyPlayerType struct{}

func (tickPolicyPlayerType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &testEntity{handle: handle, data: data}
}

func (tickPolicyPlayerType) EncodeEntity() string { return "minecraft:player" }
func (tickPolicyPlayerType) BBox(Entity) cube.BBox {
	return cube.Box(0, 0, 0, 1, 1, 1)
}
func (tickPolicyPlayerType) DecodeNBT(map[string]any, *EntityData) {}
func (tickPolicyPlayerType) EncodeNBT(*EntityData) map[string]any  { return nil }

type countingRandSource struct {
	calls int
}

func (source *countingRandSource) Uint64() uint64 {
	source.calls++
	return uint64(source.calls) * 0x9e3779b97f4a7c15
}

var _ rand.Source = (*countingRandSource)(nil)
