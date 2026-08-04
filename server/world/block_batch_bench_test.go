package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

const (
	benchmarkBlockChanges = 123
	benchmarkViewers      = 150
)

var benchmarkSetOpts = &SetOpts{
	DisableBlockUpdates:       true,
	DisableLiquidDisplacement: true,
	DisableRedstoneUpdates:    true,
}

func BenchmarkSparseBlockUpdates(b *testing.B) {
	b.Run("SetBlockLoop", func(b *testing.B) {
		benchmarkSparseBlockUpdates(b, false)
	})
	b.Run("SetBlocks", func(b *testing.B) {
		benchmarkSparseBlockUpdates(b, true)
	})
}

func benchmarkSparseBlockUpdates(b *testing.B, bulk bool) {
	w := Config{Synchronous: true}.New()
	stone, ok := w.conf.Blocks.BlockByName("minecraft:stone", nil)
	if !ok {
		b.Fatal("stone is not registered")
	}

	changes := make([]BlockChange, 0, benchmarkBlockChanges)
	for index := range benchmarkBlockChanges {
		changes = append(changes, BlockChange{
			Position: cube.Pos{index % 8, 64 + (index / 64), (index / 8) % 8},
			Block:    stone,
		})
	}
	viewer := &blockBatchBenchmarkViewer{}
	w.Do(func(tx *Tx) {
		column := tx.chunk(ChunkPos{})
		for range benchmarkViewers {
			column.viewers = append(column.viewers, viewer)
		}
	})
	b.Cleanup(func() {
		w.Do(func(tx *Tx) {
			tx.chunk(ChunkPos{}).viewers = nil
		})
		_ = w.Close()
	})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Do(func(tx *Tx) {
			if bulk {
				tx.SetBlocks(changes, benchmarkSetOpts)
				return
			}
			for _, change := range changes {
				tx.SetBlock(change.Position, change.Block, benchmarkSetOpts)
			}
		})
	}
	b.StopTimer()

	iterations := float64(b.N)
	b.ReportMetric(float64(viewer.singleCalls)/iterations, "single-callbacks/op")
	b.ReportMetric(float64(viewer.batchCalls)/iterations, "batch-callbacks/op")
	b.ReportMetric(float64(viewer.updates)/iterations, "updates/op")
}

type blockBatchBenchmarkViewer struct {
	NopViewer
	singleCalls int
	batchCalls  int
	updates     int
}

func (viewer *blockBatchBenchmarkViewer) ViewBlockUpdate(cube.Pos, Block, int) {
	viewer.singleCalls++
	viewer.updates++
}

func (viewer *blockBatchBenchmarkViewer) ViewBlockUpdates(_ SubChunkPos, updates []BlockUpdate) {
	viewer.batchCalls++
	viewer.updates += len(updates)
}
