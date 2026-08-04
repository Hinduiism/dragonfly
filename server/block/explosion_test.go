package block

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestCompiledExplosionSelectionMatchesLiveSelection(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
		loadExplosionSelectionChunks(t, tx, origin, size)
		buildExplosionSelectionScene(tx)

		volume, ok := compileExplosionBlastVolume(tx, origin, size)
		if !ok {
			t.Fatal("expected test scene to support compiled selection")
		}
		strengths := fixedExplosionStrengths(size)
		want := selectExplosionBlocksLive(tx, origin, size, nil, strengths, 512)
		for _, workers := range []int{1, 2, 4, 8} {
			results := calculateExplosionRaysWithWorkers(volume, origin, strengths, workers)
			for _, result := range results {
				if !result.complete {
					t.Fatalf("compiled selection with %d workers escaped its compiled volume", workers)
				}
			}
			got := mergeExplosionRayResults(volume, results, 512)
			if !slices.Equal(got, want) {
				t.Fatalf("compiled selection with %d workers differs from live selection\ncompiled=%v\nlive=%v", workers, got, want)
			}
		}
	})
}

func TestCompiledExplosionSelectionPreservesRandomStream(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
		loadExplosionSelectionChunks(t, tx, origin, size)
		buildExplosionSelectionScene(tx)

		compiledRandom := rand.New(rand.NewPCG(10, 20))
		liveRandom := rand.New(rand.NewPCG(10, 20))
		got := selectExplosionBlocks(tx, origin, size, compiledRandom, 512)
		want := selectExplosionBlocksLive(tx, origin, size, liveRandom, nil, 512)
		if !slices.Equal(got, want) {
			t.Fatal("compiled selection changed affected block ordering")
		}
		if compiledRandom.Float64() != liveRandom.Float64() {
			t.Fatal("compiled selection consumed a different random sequence")
		}
	})
}

func TestCompiledExplosionSelectionBoundsContainMaximumRays(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		for _, test := range []struct {
			origin mgl64.Vec3
			size   float64
		}{
			{origin: mgl64.Vec3{8.5, 65, 8.5}, size: 0.1},
			{origin: mgl64.Vec3{15.99, 72.25, 15.01}, size: 1},
			{origin: mgl64.Vec3{-0.01, 64.75, -15.99}, size: 2},
			{origin: mgl64.Vec3{-24.5, 80, 31.5}, size: 4.5},
			{origin: mgl64.Vec3{128.125, 40.875, -96.625}, size: 6},
			{origin: mgl64.Vec3{-160.5, 100, 160.5}, size: 12},
		} {
			loadExplosionSelectionChunks(t, tx, test.origin, test.size)
			volume, ok := compileExplosionBlastVolume(tx, test.origin, test.size)
			if !ok {
				t.Fatalf("expected compiled volume for origin=%v size=%v", test.origin, test.size)
			}
			strengths := make([]float64, len(rays))
			for i := range strengths {
				strengths[i] = test.size * 1.3
			}
			for _, result := range calculateExplosionRaysWithWorkers(volume, test.origin, strengths, 8) {
				if !result.complete {
					t.Fatalf("maximum-strength ray escaped bounds for origin=%v size=%v", test.origin, test.size)
				}
			}
		}
	})
}

func TestCompiledExplosionSelectionDoesNotLoadMissingChunks(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
		tx.Block(cube.Pos{0, 65, 0})
		missing := cube.Pos{0, 65, 16}
		if _, loaded := tx.BlockLoaded(missing); loaded {
			t.Fatal("expected neighbouring chunk to start unloaded")
		}
		if _, ok := compileExplosionBlastVolume(tx, origin, size); ok {
			t.Fatal("expected compilation to fall back when a required chunk is missing")
		}
		if _, loaded := tx.BlockLoaded(missing); loaded {
			t.Fatal("compiled explosion unexpectedly loaded a missing chunk")
		}
	})
}

func TestExplosionExposureMatchesOriginalImplementation(t *testing.T) {
	for _, test := range []struct {
		name            string
		suppressLiquids bool
		build           func(*world.Tx)
	}{
		{name: "air"},
		{name: "solid wall", build: func(tx *world.Tx) {
			for y := 63; y <= 67; y++ {
				tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, explosionTestSetOpts())
			}
		}},
		{name: "partial slab", build: func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{10, 64, 8}, Slab{Block: Stone{}}, explosionTestSetOpts())
		}},
		{name: "suppressing liquid", suppressLiquids: true, build: func(tx *world.Tx) {
			for y := 63; y <= 67; y++ {
				tx.SetLiquid(cube.Pos{10, y, 8}, Water{Depth: 8})
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()

			w.Do(func(tx *world.Tx) {
				origin := mgl64.Vec3{8.5, 65, 8.5}
				entity := addExplosionTestEntity(t, tx, mgl64.Vec3{13, 64, 8.5})
				loadExplosionCollisionChunks(t, tx, origin, []world.Entity{entity})
				if test.build != nil {
					test.build(tx)
				}

				config := ExplosionConfig{SuppressUnderwaterImpact: test.suppressLiquids}
				want := originalExplosionExposure(config, tx, origin, entity)
				if got := config.exposure(tx, origin, entity); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("cached live exposure differs from original: got %v, want %v", got, want)
				}

				volume, ok := compileExplosionCollisionVolume(tx, origin, []world.Entity{entity}, test.suppressLiquids)
				if !ok {
					t.Fatal("expected test scene to support compiled exposure")
				}
				got, complete := config.compiledExposure(volume, origin, entity)
				if !complete {
					t.Fatal("compiled exposure traversal escaped its snapshot")
				}
				if math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("compiled exposure differs from original: got %v, want %v", got, want)
				}
			})
		})
	}
}

func TestExplosionCollisionVolumeInvalidatedByBlockMutation(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin := mgl64.Vec3{8.5, 65, 8.5}
		entity := addExplosionTestEntity(t, tx, mgl64.Vec3{13, 64, 8.5})
		entities := []world.Entity{entity}
		loadExplosionCollisionChunks(t, tx, origin, entities)

		before, ok := compileExplosionCollisionVolume(tx, origin, entities, false)
		if !ok || !before.snapshot.Current(tx) {
			t.Fatal("expected a current collision volume")
		}
		for y := 63; y <= 67; y++ {
			tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, explosionTestSetOpts())
		}
		if before.snapshot.Current(tx) {
			t.Fatal("expected block mutation to invalidate the collision volume")
		}

		after, ok := compileExplosionCollisionVolume(tx, origin, entities, false)
		if !ok || !after.snapshot.Current(tx) {
			t.Fatal("expected collision volume to rebuild from the mutated world")
		}
		config := ExplosionConfig{}
		want := originalExplosionExposure(config, tx, origin, entity)
		got, complete := config.compiledExposure(after, origin, entity)
		if !complete || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("rebuilt exposure differs from original: got %v, want %v, complete=%v", got, want, complete)
		}
	})
}

func TestExplosionCollisionRayMatchesTraverseBlocks(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		origin := mgl64.Vec3{8.5, 65, 8.5}
		entities := []world.Entity{
			addExplosionTestEntity(t, tx, mgl64.Vec3{2, 64, 2}),
			addExplosionTestEntity(t, tx, mgl64.Vec3{14, 64, 2}),
			addExplosionTestEntity(t, tx, mgl64.Vec3{2, 64, 14}),
			addExplosionTestEntity(t, tx, mgl64.Vec3{14, 64, 14}),
		}
		loadExplosionCollisionChunks(t, tx, origin, entities)
		for y := 63; y <= 66; y++ {
			tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, explosionTestSetOpts())
		}
		tx.SetBlock(cube.Pos{7, 64, 11}, Slab{Block: Stone{}, Top: true}, explosionTestSetOpts())
		tx.SetLiquid(cube.Pos{5, 64, 5}, Water{Depth: 8})

		volume, ok := compileExplosionCollisionVolume(tx, origin, entities, true)
		if !ok {
			t.Fatal("expected collision volume")
		}
		endpoints := []mgl64.Vec3{
			{2, 64, 2},
			{14, 64, 14},
			{10, 65, 8},
			{8.5, 63, 8.5},
			{8.5, 66, 8.5},
			{8, 64, 8},
		}
		random := rand.New(rand.NewPCG(100, 200))
		for range 1000 {
			endpoints = append(endpoints, mgl64.Vec3{
				2 + random.Float64()*12,
				63 + random.Float64()*3,
				2 + random.Float64()*12,
			})
		}

		for index, endpoint := range endpoints {
			var wantCollision, wantComplete bool
			trace.TraverseBlocks(origin, endpoint, func(pos cube.Pos) bool {
				wantCollision, wantComplete = volume.intersects(pos, origin, endpoint, true)
				return wantComplete && !wantCollision
			})
			gotCollision, gotComplete := volume.rayIntersects(origin, endpoint, true)
			if gotCollision != wantCollision || gotComplete != wantComplete {
				t.Fatalf("ray %d to %v differs: got collision=%v complete=%v, want collision=%v complete=%v", index, endpoint, gotCollision, gotComplete, wantCollision, wantComplete)
			}
		}
	})
}

func TestExplosionBooleanBlockIntersectionMatchesOriginal(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		for _, test := range []struct {
			name  string
			block world.Block
		}{
			{name: "solid", block: Stone{}},
			{name: "bottom slab", block: Slab{Block: Stone{}}},
			{name: "top slab", block: Slab{Block: Stone{}, Top: true}},
		} {
			t.Run(test.name, func(t *testing.T) {
				segments := [][2]mgl64.Vec3{
					{{0.2, 64.2, 0.2}, {0.8, 64.8, 0.8}},
					{{0.5, 64.5, 0.5}, {2, 64.5, 0.5}},
					{{-1, 64.5, 0.5}, {0.5, 64.5, 0.5}},
					{{-1, 64, 0}, {2, 64, 0}},
					{{-1, 65, 1}, {2, 65, 1}},
				}
				random := rand.New(rand.NewPCG(300, 400))
				for range 5000 {
					segments = append(segments, [2]mgl64.Vec3{
						{-1 + random.Float64()*3, 63 + random.Float64()*3, -1 + random.Float64()*3},
						{-1 + random.Float64()*3, 63 + random.Float64()*3, -1 + random.Float64()*3},
					})
				}
				for index, segment := range segments {
					_, want := trace.BlockIntercept(pos, tx, test.block, segment[0], segment[1])
					var got bool
					for _, box := range test.block.Model().BBox(pos, tx) {
						if explosionBBoxBoundaryIntersects(box.Translate(pos.Vec3()), segment[0], segment[1]) {
							got = true
							break
						}
					}
					if got != want {
						t.Fatalf("segment %d from %v to %v differs: got %v, want %v", index, segment[0], segment[1], got, want)
					}
				}
			})
		}
	})
}

func TestExplosionExposureUsesPostHandlerWorldState(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
	var want float64
	w.Handle(explosionTestHandler{handleExplosion: func(ctx *world.Context, src world.ExplosionSource, entities *[]world.Entity, blocks *[]cube.Pos, _ *float64, spawnFire *bool) {
		for y := 63; y <= 67; y++ {
			ctx.SetBlock(cube.Pos{10, y, 8}, Stone{}, explosionTestSetOpts())
		}
		*blocks = nil
		*spawnFire = false
		if len(*entities) != 0 {
			config := ExplosionConfig{}
			exposure := originalExplosionExposure(config, ctx.Tx, origin, (*entities)[0])
			want = (1 - (*entities)[0].Position().Sub(origin).Len()/(size*2)) * exposure
		}
	}})

	w.Do(func(tx *world.Tx) {
		impacts := make([]float64, 0, 24)
		for range 24 {
			addExplosionTestEntityWithCallback(t, tx, mgl64.Vec3{13, 64, 8.5}, func(impact float64) {
				impacts = append(impacts, impact)
			})
		}
		ExplosionConfig{RandSource: rand.NewPCG(1, 2)}.Explode(tx, explosionTestSource{position: origin, size: size})
		if len(impacts) != 24 {
			t.Fatalf("expected 24 entity callbacks, got %d", len(impacts))
		}
		for index, impact := range impacts {
			if math.Float64bits(impact) != math.Float64bits(want) {
				t.Fatalf("impact %d used pre-handler world state: got %v, want %v", index, impact, want)
			}
		}
	})
}

func TestExplosionExposureRebuildsAfterEntityMutation(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	origin, size := mgl64.Vec3{8.5, 65, 8.5}, 4.5
	var ordered []world.Entity
	w.Handle(explosionTestHandler{handleExplosion: func(_ *world.Context, _ world.ExplosionSource, entities *[]world.Entity, blocks *[]cube.Pos, _ *float64, spawnFire *bool) {
		*entities = ordered
		*blocks = nil
		*spawnFire = false
	}})

	w.Do(func(tx *world.Tx) {
		impacts := make([]float64, 24)
		position := mgl64.Vec3{13, 64, 8.5}
		distanceFactor := 1 - position.Sub(origin).Len()/(size*2)
		var blockedImpact float64
		for index := range 24 {
			index := index
			entity := addExplosionTestEntityWithCallback(t, tx, position, func(impact float64) {
				impacts[index] = impact
				if index != 0 {
					return
				}
				for y := 63; y <= 67; y++ {
					tx.SetBlock(cube.Pos{10, y, 8}, Stone{}, explosionTestSetOpts())
				}
				blockedImpact = distanceFactor * originalExplosionExposure(ExplosionConfig{}, tx, origin, ordered[1])
			})
			ordered = append(ordered, entity)
		}

		ExplosionConfig{RandSource: rand.NewPCG(3, 4)}.Explode(tx, explosionTestSource{position: origin, size: size})
		if math.Float64bits(impacts[0]) != math.Float64bits(distanceFactor) {
			t.Fatalf("first entity did not use the original air volume: got %v, want %v", impacts[0], distanceFactor)
		}
		for index, impact := range impacts[1:] {
			if math.Float64bits(impact) != math.Float64bits(blockedImpact) {
				t.Fatalf("impact %d used stale pre-mutation collision data: got %v, want %v", index+1, impact, blockedImpact)
			}
		}
	})
}

func loadExplosionSelectionChunks(t testing.TB, tx *world.Tx, origin mgl64.Vec3, size float64) {
	t.Helper()
	minPos, maxPos, ok := explosionSelectionBounds(origin, size)
	if !ok {
		t.Fatal("expected valid explosion selection bounds")
	}
	for chunkX := minPos[0] >> 4; chunkX <= maxPos[0]>>4; chunkX++ {
		for chunkZ := minPos[2] >> 4; chunkZ <= maxPos[2]>>4; chunkZ++ {
			tx.Block(cube.Pos{chunkX << 4, int(origin[1]), chunkZ << 4})
		}
	}
}

func loadExplosionCollisionChunks(t testing.TB, tx *world.Tx, origin mgl64.Vec3, entities []world.Entity) {
	t.Helper()
	minPos, maxPos, ok := explosionCollisionBounds(origin, entities)
	if !ok {
		t.Fatal("expected valid explosion collision bounds")
	}
	for chunkX := minPos[0] >> 4; chunkX <= maxPos[0]>>4; chunkX++ {
		for chunkZ := minPos[2] >> 4; chunkZ <= maxPos[2]>>4; chunkZ++ {
			tx.Block(cube.Pos{chunkX << 4, int(origin[1]), chunkZ << 4})
		}
	}
}

func buildExplosionSelectionScene(tx *world.Tx) {
	for x := 2; x <= 14; x++ {
		for z := 2; z <= 14; z++ {
			tx.SetBlock(cube.Pos{x, 62, z}, Stone{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
		}
	}
	for y := 63; y <= 67; y++ {
		tx.SetBlock(cube.Pos{11, y, 8}, Cobblestone{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	}
	tx.SetLiquid(cube.Pos{6, 65, 8}, Water{Depth: 8})
}

func fixedExplosionStrengths(size float64) []float64 {
	r := rand.New(rand.NewPCG(30, 40))
	strengths := make([]float64, len(rays))
	for i := range strengths {
		strengths[i] = size * (0.7 + r.Float64()*0.6)
	}
	return strengths
}

func originalExplosionExposure(config ExplosionConfig, tx *world.Tx, origin mgl64.Vec3, entity world.Entity) float64 {
	box := entity.H().Type().BBox(entity).Translate(entity.Position())
	boxMin, boxMax := box.Min(), box.Max()
	diff := boxMax.Sub(boxMin).Mul(2.0).Add(mgl64.Vec3{1, 1, 1})
	step := mgl64.Vec3{1.0 / diff[0], 1.0 / diff[1], 1.0 / diff[2]}
	if step[0] < 0 || step[1] < 0 || step[2] < 0 {
		return 0
	}
	xOffset := (1.0 - math.Floor(diff[0])/diff[0]) / 2.0
	zOffset := (1.0 - math.Floor(diff[2])/diff[2]) / 2.0

	var checks, misses float64
	for x := 0.0; x <= 1.0; x += step[0] {
		for y := 0.0; y <= 1.0; y += step[1] {
			for z := 0.0; z <= 1.0; z += step[2] {
				point := mgl64.Vec3{
					lerp(x, boxMin[0], boxMax[0]) + xOffset,
					lerp(y, boxMin[1], boxMax[1]),
					lerp(z, boxMin[2], boxMax[2]) + zOffset,
				}
				var collided bool
				trace.TraverseBlocks(origin, point, func(pos cube.Pos) bool {
					if config.SuppressUnderwaterImpact {
						if _, liquid := tx.Liquid(pos); liquid {
							collided = true
							return false
						}
					}
					_, collided = trace.BlockIntercept(pos, tx, tx.Block(pos), origin, point)
					return !collided
				})
				if !collided {
					misses++
				}
				checks++
			}
		}
	}
	return misses / checks
}

func addExplosionTestEntity(t testing.TB, tx *world.Tx, position mgl64.Vec3) world.Entity {
	return addExplosionTestEntityWithCallback(t, tx, position, nil)
}

func addExplosionTestEntityWithCallback(t testing.TB, tx *world.Tx, position mgl64.Vec3, onExplode func(float64)) world.Entity {
	t.Helper()
	handle := world.EntitySpawnOpts{Position: position}.New(explosionTestEntityType{}, explosionTestEntityConfig{onExplode: onExplode})
	tx.AddEntity(handle)
	entity, ok := handle.Entity(tx)
	if !ok {
		t.Fatal("expected test entity to be available")
	}
	return entity
}

func explosionTestSetOpts() *world.SetOpts {
	return &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
}

type explosionTestEntityConfig struct {
	onExplode func(float64)
}

func (config explosionTestEntityConfig) Apply(data *world.EntityData) {
	data.Data = config.onExplode
}

type explosionTestEntityType struct{}

func (explosionTestEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	onExplode, _ := data.Data.(func(float64))
	return &explosionTestEntity{handle: handle, data: data, onExplode: onExplode}
}
func (explosionTestEntityType) EncodeEntity() string { return "dragonfly:test_explosion_entity" }
func (explosionTestEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (explosionTestEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (explosionTestEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type explosionTestEntity struct {
	handle    *world.EntityHandle
	data      *world.EntityData
	onExplode func(float64)
}

func (entity *explosionTestEntity) H() *world.EntityHandle  { return entity.handle }
func (entity *explosionTestEntity) Position() mgl64.Vec3    { return entity.data.Pos }
func (entity *explosionTestEntity) Rotation() cube.Rotation { return entity.data.Rot }
func (*explosionTestEntity) Close() error                   { return nil }
func (entity *explosionTestEntity) Explode(_ world.ExplosionSource, impact float64) {
	if entity.onExplode != nil {
		entity.onExplode(impact)
	}
}

type explosionTestSource struct {
	position mgl64.Vec3
	size     float64
}

func (source explosionTestSource) Position() mgl64.Vec3 { return source.position }
func (source explosionTestSource) Size() float64        { return source.size }

type explosionTestHandler struct {
	world.NopHandler
	handleExplosion func(*world.Context, world.ExplosionSource, *[]world.Entity, *[]cube.Pos, *float64, *bool)
}

func (handler explosionTestHandler) HandleExplosion(ctx *world.Context, source world.ExplosionSource, entities *[]world.Entity, blocks *[]cube.Pos, itemDropChance *float64, spawnFire *bool) {
	if handler.handleExplosion != nil {
		handler.handleExplosion(ctx, source, entities, blocks, itemDropChance, spawnFire)
	}
}
