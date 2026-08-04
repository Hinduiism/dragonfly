package block

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func BenchmarkExplosionBlockSelection(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
		loadExplosionSelectionChunks(b, tx, origin, size)
		buildExplosionSelectionScene(tx)
		strengths := fixedExplosionStrengths(size)

		b.Run("live", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				selectExplosionBlocksLive(tx, origin, size, nil, strengths, 512)
			}
		})
		b.Run("compiled_complete", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				volume, ok := compileExplosionBlastVolume(tx, origin, size)
				if !ok {
					b.Fatal("expected compiled blast volume")
				}
				results := calculateExplosionRaysWithWorkers(volume, origin, strengths, 1)
				mergeExplosionRayResults(volume, results, 512)
			}
		})
		b.Run("compiled_complete_parallel", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				volume, ok := compileExplosionBlastVolume(tx, origin, size)
				if !ok {
					b.Fatal("expected compiled blast volume")
				}
				results := calculateExplosionRaysWithWorkers(volume, origin, strengths, 8)
				mergeExplosionRayResults(volume, results, 512)
			}
		})
		b.Run("compiled_calculation", func(b *testing.B) {
			volume, ok := compileExplosionBlastVolume(tx, origin, size)
			if !ok {
				b.Fatal("expected compiled blast volume")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				results := calculateExplosionRaysWithWorkers(volume, origin, strengths, 1)
				mergeExplosionRayResults(volume, results, 512)
			}
		})
	})
}

func BenchmarkExplosionBlockSelectionSizes(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin := mgl64.Vec3{8.5, 65, 8.5}
		loadExplosionSelectionChunks(b, tx, origin, 6)
		buildExplosionSelectionScene(tx)
		for _, test := range []struct {
			name string
			size float64
		}{
			{name: "size_1", size: 1},
			{name: "size_2", size: 2},
			{name: "size_4_5", size: 4.5},
			{name: "size_6", size: 6},
		} {
			strengths := fixedExplosionStrengths(test.size)
			b.Run(test.name+"/live", func(b *testing.B) {
				for b.Loop() {
					selectExplosionBlocksLive(tx, origin, test.size, nil, strengths, 512)
				}
			})
			b.Run(test.name+"/compiled", func(b *testing.B) {
				for b.Loop() {
					volume, ok := compileExplosionBlastVolume(tx, origin, test.size)
					if !ok {
						b.Fatal("expected compiled blast volume")
					}
					results := calculateExplosionRaysWithWorkers(volume, origin, strengths, min(8, runtime.GOMAXPROCS(0)))
					mergeExplosionRayResults(volume, results, 512)
				}
			})
		}
	})
}

func BenchmarkExplosionExposure(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin := mgl64.Vec3{8.5, 65, 8.5}
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{13, 64, 8.5}}.New(explosionTestEntityType{}, explosionTestEntityConfig{})
		tx.AddEntity(handle)
		entity, ok := handle.Entity(tx)
		if !ok {
			b.Fatal("expected test entity")
		}
		for y := 63; y <= 67; y++ {
			tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
		}
		config := ExplosionConfig{}

		b.Run("live", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				config.exposure(tx, origin, entity)
			}
		})
		b.Run("compiled_complete", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				volume, ok := compileExplosionCollisionVolume(tx, origin, []world.Entity{entity}, false)
				if !ok {
					b.Fatal("expected compiled collision volume")
				}
				if _, complete := config.compiledExposure(volume, origin, entity); !complete {
					b.Fatal("expected complete compiled exposure")
				}
			}
		})
		b.Run("compiled_calculation", func(b *testing.B) {
			volume, ok := compileExplosionCollisionVolume(tx, origin, []world.Entity{entity}, false)
			if !ok {
				b.Fatal("expected compiled collision volume")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, complete := config.compiledExposure(volume, origin, entity); !complete {
					b.Fatal("expected complete compiled exposure")
				}
			}
		})
	})
}

func BenchmarkExplosionExposureGroup(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin := mgl64.Vec3{8.5, 65, 8.5}
		entities := make([]world.Entity, 0, 150)
		for i := range 150 {
			angle := float64(i) * (2 * math.Pi / 150)
			radius := 5.0 + float64(i%3)
			position := mgl64.Vec3{origin[0] + radius*math.Cos(angle), 64, origin[2] + radius*math.Sin(angle)}
			handle := world.EntitySpawnOpts{Position: position}.New(explosionTestEntityType{}, explosionTestEntityConfig{})
			tx.AddEntity(handle)
			entity, ok := handle.Entity(tx)
			if !ok {
				b.Fatal("expected test entity")
			}
			entities = append(entities, entity)
		}
		minPos, maxPos, ok := explosionCollisionBounds(origin, entities)
		if !ok {
			b.Fatal("expected collision bounds")
		}
		for chunkX := minPos[0] >> 4; chunkX <= maxPos[0]>>4; chunkX++ {
			for chunkZ := minPos[2] >> 4; chunkZ <= maxPos[2]>>4; chunkZ++ {
				tx.Block(cube.Pos{chunkX << 4, int(origin[1]), chunkZ << 4})
			}
		}
		for y := 63; y <= 67; y++ {
			tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
		}
		config := ExplosionConfig{}

		for _, count := range []int{10, 50, 150} {
			group := entities[:count]
			b.Run(fmt.Sprintf("entities_%d/live", count), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					for _, entity := range group {
						config.exposure(tx, origin, entity)
					}
				}
			})
			b.Run(fmt.Sprintf("entities_%d/compiled_complete", count), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					volume, ok := compileExplosionCollisionVolume(tx, origin, group, false)
					if !ok {
						b.Fatal("expected compiled collision volume")
					}
					for _, entity := range group {
						if _, complete := config.compiledExposure(volume, origin, entity); !complete {
							b.Fatal("expected complete compiled exposure")
						}
					}
				}
			})
		}
	})
}
