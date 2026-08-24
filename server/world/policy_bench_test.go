package world

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

func BenchmarkWorldTickPolicy(b *testing.B) {
	for _, test := range []struct {
		name   string
		policy TickPolicy
	}{
		{name: "default"},
		{name: "static", policy: TickPolicy{Disabled: TickAllSubsystems}},
	} {
		b.Run(test.name, func(b *testing.B) {
			w := Config{Synchronous: true, TickPolicy: test.policy}.New()
			b.Cleanup(func() { _ = w.Close() })
			w.Do(func(tx *Tx) {
				for x := int32(-2); x <= 2; x++ {
					for z := int32(-2); z <= 2; z++ {
						tx.chunk(ChunkPos{x, z})
					}
				}
			})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.AdvanceTick()
			}
		})
	}
}

func BenchmarkBlockTickPolicy(b *testing.B) {
	for _, test := range []struct {
		name                         string
		random, blockEntitiesEnabled bool
	}{
		{name: "both", random: true, blockEntitiesEnabled: true},
		{name: "random", random: true},
		{name: "block_entities", blockEntitiesEnabled: true},
		{name: "neither"},
	} {
		b.Run(test.name, func(b *testing.B) {
			w := Config{Synchronous: true}.New()
			b.Cleanup(func() { _ = w.Close() })
			w.Do(func(tx *Tx) {
				for x := int32(-2); x <= 2; x++ {
					for z := int32(-2); z <= 2; z++ {
						tx.chunk(ChunkPos{x, z})
					}
				}
			})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Do(func(tx *Tx) {
					ticker{}.tickBlocks(tx, nil, tx.CurrentTick(), test.random, test.blockEntitiesEnabled)
				})
			}
		})
	}
}

func BenchmarkBlockMutationPolicy(b *testing.B) {
	for _, test := range []struct {
		name   string
		policy TickPolicy
	}{
		{name: "default"},
		{name: "no_neighbours_or_redstone", policy: TickPolicy{Disabled: TickNeighbourUpdates | TickRedstone}},
	} {
		b.Run(test.name, func(b *testing.B) {
			w := Config{Synchronous: true, TickPolicy: test.policy}.New()
			b.Cleanup(func() { _ = w.Close() })
			stone, ok := w.conf.Blocks.BlockByName("minecraft:stone", nil)
			if !ok {
				b.Fatal("stone block is not registered")
			}
			changes := make([]BlockChange, 128)
			for i := range changes {
				changes[i] = BlockChange{Position: cube.Pos{i, 4, 0}, Block: stone}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Do(func(tx *Tx) {
					tx.SetBlocks(changes, nil)
					w.neighbourUpdates = w.neighbourUpdates[:0]
					clear(w.redstone.dirty)
				})
			}
		})
	}
}

func BenchmarkNonPlayerEntityTickPolicy(b *testing.B) {
	for _, enabled := range []bool{true, false} {
		b.Run(fmt.Sprintf("enabled_%t", enabled), func(b *testing.B) {
			policy := TickPolicy{}
			if !enabled {
				policy.Disabled = TickNonPlayerEntities
			}
			w := Config{Synchronous: true, TickPolicy: policy}.New()
			b.Cleanup(func() { _ = w.Close() })
			w.Do(func(tx *Tx) {
				for i := range 128 {
					handle := EntitySpawnOpts{Position: mgl64.Vec3{float64(i % 8), 4, float64(i / 8)}}.New(benchmarkEntityType{}, benchmarkEntityConfig{})
					tx.AddEntity(handle)
				}
			})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.AdvanceTick()
			}
		})
	}
}

func BenchmarkEntityStorageColumnConversion(b *testing.B) {
	for _, mode := range []EntityStorageMode{EntityStoragePersistent, EntityStorageTransient} {
		b.Run(fmt.Sprintf("mode_%d", mode), func(b *testing.B) {
			w := Config{Synchronous: true, EntityStorage: mode}.New()
			b.Cleanup(func() { _ = w.Close() })
			col := &Column{
				Chunk:         chunk.New(w.conf.Blocks, w.Range()),
				BlockEntities: make(map[cube.Pos]Block),
				Entities:      make([]*EntityHandle, 128),
			}
			for i := range col.Entities {
				col.Entities[i] = EntitySpawnOpts{Position: mgl64.Vec3{float64(i), 4, 0}}.New(benchmarkEntityType{}, benchmarkEntityConfig{})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = w.columnTo(col, ChunkPos{})
			}
		})
	}
}

func BenchmarkLoaderQueueConstruction(b *testing.B) {
	for _, radius := range []int{4, 8, 12} {
		b.Run(fmt.Sprintf("radius_%d", radius), func(b *testing.B) {
			w := Config{Synchronous: true}.New()
			b.Cleanup(func() { _ = w.Close() })
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				loader := NewLoader(radius, w, NopViewer{})
				w.Do(func(tx *Tx) { loader.Close(tx) })
			}
		})
	}
}

func BenchmarkLoaderWorldRadiusTransfer(b *testing.B) {
	low := Config{Synchronous: true, MaxChunkRadius: 4}.New()
	high := Config{Synchronous: true, MaxChunkRadius: 12}.New()
	loader := NewLoader(12, high, NopViewer{})
	b.Cleanup(func() {
		high.Do(func(tx *Tx) { loader.Close(tx) })
		_ = low.Close()
		_ = high.Close()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		loader.ChangeWorldAndRadius(nil, low, 4)
		loader.ChangeWorldAndRadius(nil, high, 12)
	}
}

type benchmarkEntityConfig struct{}

func (benchmarkEntityConfig) Apply(*EntityData) {}

type benchmarkEntityType struct{}

func (benchmarkEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return benchmarkEntity{handle: handle, data: data}
}

func (benchmarkEntityType) EncodeEntity() string                  { return "dragonfly:benchmark_entity" }
func (benchmarkEntityType) BBox(Entity) cube.BBox                 { return cube.BBox{} }
func (benchmarkEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (benchmarkEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type benchmarkEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (entity benchmarkEntity) H() *EntityHandle        { return entity.handle }
func (entity benchmarkEntity) Position() mgl64.Vec3    { return entity.data.Pos }
func (entity benchmarkEntity) Rotation() cube.Rotation { return entity.data.Rot }
func (benchmarkEntity) Close() error                   { return nil }
func (benchmarkEntity) Tick(*Tx, int64)                {}
