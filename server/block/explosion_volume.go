package block

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	explosionRayStepDecay         = 0.225
	explosionRayStepLength        = 0.3
	explosionSelectionMargin      = 1.0
	explosionParallelCellCutoff   = 2048
	explosionExposureSampleCutoff = 1024
	maxExplosionWorkers           = 8
)

const (
	explosionCollisionFull uint8 = 1 << iota
	explosionCollisionLiquid
	explosionCollisionCached
)

type explosionBlockInfo struct {
	resistance float64
	flags      uint8
}

const (
	explosionBlockResists uint8 = 1 << iota
	explosionBlockStopsRay
	explosionBlockAffected
	explosionBlockKnown
)

type explosionBlastVolume struct {
	min                 cube.Pos
	sizeX, sizeY, sizeZ int
	cells               []explosionBlockInfo
}

type explosionMaterialKey struct {
	kind        uint8
	base, state uint64
}

type explosionRayResult struct {
	cells    []uint32
	complete bool
}

type explosionCollisionCell struct {
	boxStart uint32
	boxCount uint16
	flags    uint8
}

type explosionCollisionVolume struct {
	snapshot            *world.BlockSnapshot
	min                 cube.Pos
	sizeX, sizeY, sizeZ int
	cells               []explosionCollisionCell
	boxes               []cube.BBox
}

type explosionSnapshotSource struct {
	snapshot *world.BlockSnapshot
	complete bool
}

type liveExplosionCollisionCache struct {
	min                 cube.Pos
	sizeX, sizeY, sizeZ int
	cells               []explosionCollisionCell
	boxes               []cube.BBox
}

// rays contains the fixed Bedrock explosion directions in their canonical
// order. The order is also the deterministic merge order for compiled rays.
var rays = make([]mgl64.Vec3, 0, 1352)

var explosionWorkerBudget = make(chan struct{}, max(0, min(maxExplosionWorkers-1, runtime.GOMAXPROCS(0)-1)))

func init() {
	for x := 0.0; x < 16; x++ {
		for y := 0.0; y < 16; y++ {
			for z := 0.0; z < 16; z++ {
				if x != 0 && x != 15 && y != 0 && y != 15 && z != 0 && z != 15 {
					continue
				}
				rays = append(rays, mgl64.Vec3{x/15*2 - 1, y/15*2 - 1, z/15*2 - 1}.Normalize().Mul(explosionRayStepLength))
			}
		}
	}
}

func selectExplosionBlocks(tx *world.Tx, origin mgl64.Vec3, size float64, r *rand.Rand, estimatedBlocks int) []cube.Pos {
	volume, ok := compileExplosionBlastVolume(tx, origin, size)
	if !ok {
		return selectExplosionBlocksLive(tx, origin, size, r, nil, estimatedBlocks)
	}

	strengths := make([]float64, len(rays))
	for i := range strengths {
		strengths[i] = size * (0.7 + r.Float64()*0.6)
	}
	results := calculateExplosionRays(volume, origin, strengths)
	for _, result := range results {
		if !result.complete {
			return selectExplosionBlocksLive(tx, origin, size, nil, strengths, estimatedBlocks)
		}
	}
	return mergeExplosionRayResults(volume, results, estimatedBlocks)
}

func selectExplosionBlocksLive(tx *world.Tx, origin mgl64.Vec3, size float64, r *rand.Rand, strengths []float64, estimatedBlocks int) []cube.Pos {
	affectedBlocks := make([]cube.Pos, 0, estimatedBlocks)
	blockCache := make(map[cube.Pos]explosionBlockInfo, estimatedBlocks)
	for rayIndex, ray := range rays {
		blastForce := strengthsAt(strengths, rayIndex, size, r)
		pos := origin
		for ; blastForce > 0.0; blastForce -= explosionRayStepDecay {
			current := cube.PosFromVec3(pos)
			info, ok := blockCache[current]
			if !ok {
				info = explosionBlockInformation(tx.Block(current), liquidAt(tx, current))
				blockCache[current] = info
			}
			if info.flags&explosionBlockStopsRay != 0 {
				break
			}

			pos = pos.Add(ray)
			if info.flags&explosionBlockResists != 0 {
				blastForce -= (info.resistance + 0.3) * explosionRayStepLength
			}
			if blastForce > 0 && info.flags&explosionBlockAffected == 0 {
				info.flags |= explosionBlockAffected
				blockCache[current] = info
				affectedBlocks = append(affectedBlocks, current)
			}
		}
	}
	return affectedBlocks
}

func compileExplosionBlastVolume(tx *world.Tx, origin mgl64.Vec3, size float64) (*explosionBlastVolume, bool) {
	minPos, maxPos, ok := explosionSelectionBounds(origin, size)
	if !ok {
		return nil, false
	}
	snapshot, ok := tx.SnapshotBlocks(minPos, maxPos)
	if !ok {
		return nil, false
	}

	sizeX, sizeY, sizeZ := maxPos[0]-minPos[0]+1, maxPos[1]-minPos[1]+1, maxPos[2]-minPos[2]+1
	volume := &explosionBlastVolume{
		min:   minPos,
		sizeX: sizeX,
		sizeY: sizeY,
		sizeZ: sizeZ,
		cells: make([]explosionBlockInfo, sizeX*sizeY*sizeZ),
	}
	classifications := make(map[explosionMaterialKey]explosionBlockInfo, 64)
	maximumTravelSquared := math.Pow(explosionMaximumTravel(size)+1e-7, 2)
	for y := minPos[1]; y <= maxPos[1]; y++ {
		for z := minPos[2]; z <= maxPos[2]; z++ {
			for x := minPos[0]; x <= maxPos[0]; x++ {
				pos := cube.Pos{x, y, z}
				if explosionCellDistanceSquared(origin, pos) > maximumTravelSquared {
					continue
				}
				block := snapshot.Block(pos)
				liquid, hasLiquid := snapshot.Liquid(pos)
				key := explosionMaterialHash(block, liquid, hasLiquid)
				info, cached := classifications[key]
				if !cached {
					info = explosionBlockInformation(block, liquidValue(liquid, hasLiquid))
					classifications[key] = info
				}
				index, _ := volume.index(pos)
				info.flags |= explosionBlockKnown
				volume.cells[index] = info
			}
		}
	}
	return volume, true
}

func explosionSelectionBounds(origin mgl64.Vec3, size float64) (cube.Pos, cube.Pos, bool) {
	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return cube.Pos{}, cube.Pos{}, false
	}
	reach := explosionMaximumTravel(size) + explosionSelectionMargin
	if math.IsNaN(reach) || math.IsInf(reach, 0) {
		return cube.Pos{}, cube.Pos{}, false
	}

	var minPos, maxPos cube.Pos
	const coordinateLimit = float64(1<<34 - 1)
	for axis := range 3 {
		if math.IsNaN(origin[axis]) || math.IsInf(origin[axis], 0) ||
			origin[axis]-reach < -coordinateLimit || origin[axis]+reach > coordinateLimit {
			return cube.Pos{}, cube.Pos{}, false
		}
		minPos[axis] = int(math.Floor(origin[axis] - reach))
		maxPos[axis] = int(math.Floor(origin[axis] + reach))
	}
	return minPos, maxPos, true
}

func explosionMaximumTravel(size float64) float64 {
	// Blast force is reduced through repeated floating-point subtraction. At an
	// exact mathematical multiple of the decay, rounding may leave a tiny
	// positive remainder and permit one final step.
	maxTravelSteps := max(0.0, math.Ceil(size*1.3/explosionRayStepDecay))
	return maxTravelSteps * explosionRayStepLength
}

func explosionCellDistanceSquared(origin mgl64.Vec3, pos cube.Pos) float64 {
	var distanceSquared float64
	for axis := range 3 {
		minimum, maximum := float64(pos[axis]), float64(pos[axis]+1)
		var distance float64
		switch {
		case origin[axis] < minimum:
			distance = minimum - origin[axis]
		case origin[axis] > maximum:
			distance = origin[axis] - maximum
		}
		distanceSquared += distance * distance
	}
	return distanceSquared
}

func calculateExplosionRays(volume *explosionBlastVolume, origin mgl64.Vec3, strengths []float64) []explosionRayResult {
	workerCount, acquired := acquireExplosionWorkers(volume)
	if acquired != 0 {
		defer releaseExplosionWorkers(acquired)
	}
	return calculateExplosionRaysWithWorkers(volume, origin, strengths, workerCount)
}

func calculateExplosionRaysWithWorkers(volume *explosionBlastVolume, origin mgl64.Vec3, strengths []float64, workerCount int) []explosionRayResult {
	workerCount = max(1, min(workerCount, len(rays)))
	results := make([]explosionRayResult, workerCount)
	var workers sync.WaitGroup
	for worker := 1; worker < workerCount; worker++ {
		start := worker * len(rays) / workerCount
		end := (worker + 1) * len(rays) / workerCount
		workers.Add(1)
		go func(worker, start, end int) {
			defer workers.Done()
			results[worker] = calculateExplosionRayRange(volume, origin, strengths, start, end)
		}(worker, start, end)
	}
	results[0] = calculateExplosionRayRange(volume, origin, strengths, 0, len(rays)/workerCount)
	workers.Wait()
	return results
}

func calculateExplosionRayRange(volume *explosionBlastVolume, origin mgl64.Vec3, strengths []float64, start, end int) explosionRayResult {
	result := explosionRayResult{
		cells:    make([]uint32, 0, min(len(volume.cells), 512)),
		complete: true,
	}
	seen := make([]uint64, (len(volume.cells)+63)/64)
	for rayIndex := start; rayIndex < end; rayIndex++ {
		pos := origin
		blastForce := strengths[rayIndex]
		for ; blastForce > 0.0; blastForce -= explosionRayStepDecay {
			cell, ok := volume.index(cube.PosFromVec3(pos))
			if !ok {
				result.complete = false
				return result
			}
			info := volume.cells[cell]
			if info.flags&explosionBlockKnown == 0 {
				result.complete = false
				return result
			}
			if info.flags&explosionBlockStopsRay != 0 {
				break
			}

			pos = pos.Add(rays[rayIndex])
			if info.flags&explosionBlockResists != 0 {
				blastForce -= (info.resistance + 0.3) * explosionRayStepLength
			}
			if blastForce > 0 && !explosionCellSeen(seen, cell) {
				explosionMarkCellSeen(seen, cell)
				result.cells = append(result.cells, cell)
			}
		}
	}
	return result
}

func mergeExplosionRayResults(volume *explosionBlastVolume, results []explosionRayResult, estimatedBlocks int) []cube.Pos {
	affected := make([]cube.Pos, 0, estimatedBlocks)
	seen := make([]uint64, (len(volume.cells)+63)/64)
	for _, result := range results {
		for _, cell := range result.cells {
			if explosionCellSeen(seen, cell) {
				continue
			}
			explosionMarkCellSeen(seen, cell)
			affected = append(affected, volume.position(cell))
		}
	}
	return affected
}

func explosionCellSeen(seen []uint64, cell uint32) bool {
	return seen[cell>>6]&(uint64(1)<<(cell&63)) != 0
}

func explosionMarkCellSeen(seen []uint64, cell uint32) {
	seen[cell>>6] |= uint64(1) << (cell & 63)
}

func acquireExplosionWorkers(volume *explosionBlastVolume) (workers, acquired int) {
	workers = 1
	if len(volume.cells) < explosionParallelCellCutoff {
		return workers, 0
	}
	desired := min(maxExplosionWorkers, runtime.GOMAXPROCS(0), max(1, len(rays)/128))
	for workers < desired {
		select {
		case explosionWorkerBudget <- struct{}{}:
			workers++
			acquired++
		default:
			return workers, acquired
		}
	}
	return workers, acquired
}

func releaseExplosionWorkers(count int) {
	for range count {
		<-explosionWorkerBudget
	}
}

func (volume *explosionBlastVolume) index(pos cube.Pos) (uint32, bool) {
	x, y, z := pos[0]-volume.min[0], pos[1]-volume.min[1], pos[2]-volume.min[2]
	if x < 0 || x >= volume.sizeX || y < 0 || y >= volume.sizeY || z < 0 || z >= volume.sizeZ {
		return 0, false
	}
	return uint32((y*volume.sizeZ+z)*volume.sizeX + x), true
}

func (volume *explosionBlastVolume) position(index uint32) cube.Pos {
	i := int(index)
	x := i % volume.sizeX
	i /= volume.sizeX
	z := i % volume.sizeZ
	y := i / volume.sizeZ
	return cube.Pos{volume.min[0] + x, volume.min[1] + y, volume.min[2] + z}
}

func explosionMaterialHash(block world.Block, liquid world.Liquid, hasLiquid bool) explosionMaterialKey {
	if hasLiquid {
		base, state := liquid.Hash()
		return explosionMaterialKey{kind: 1, base: base, state: state}
	}
	base, state := block.Hash()
	return explosionMaterialKey{base: base, state: state}
}

func explosionBlockInformation(block world.Block, liquid world.Liquid) explosionBlockInfo {
	if liquid != nil {
		return explosionBlockInfo{resistance: liquid.BlastResistance(), flags: explosionBlockResists}
	}
	if breakable, ok := block.(Breakable); ok {
		return explosionBlockInfo{resistance: breakable.BreakInfo().BlastResistance, flags: explosionBlockResists}
	}
	if _, air := block.(Air); !air {
		return explosionBlockInfo{flags: explosionBlockStopsRay}
	}
	return explosionBlockInfo{}
}

func liquidAt(tx *world.Tx, pos cube.Pos) world.Liquid {
	liquid, _ := tx.Liquid(pos)
	return liquid
}

func liquidValue(liquid world.Liquid, ok bool) world.Liquid {
	if !ok {
		return nil
	}
	return liquid
}

func strengthsAt(strengths []float64, index int, size float64, r *rand.Rand) float64 {
	if strengths != nil {
		return strengths[index]
	}
	return size * (0.7 + r.Float64()*0.6)
}

func compileExplosionCollisionVolume(tx *world.Tx, origin mgl64.Vec3, entities []world.Entity, suppressLiquids bool) (*explosionCollisionVolume, bool) {
	minPos, maxPos, ok := explosionCollisionBounds(origin, entities)
	if !ok {
		return nil, false
	}
	snapshot, ok := tx.SnapshotBlocks(minPos, maxPos)
	if !ok {
		return nil, false
	}

	sizeX, sizeY, sizeZ := maxPos[0]-minPos[0]+1, maxPos[1]-minPos[1]+1, maxPos[2]-minPos[2]+1
	volume := &explosionCollisionVolume{
		snapshot: snapshot,
		min:      minPos,
		sizeX:    sizeX,
		sizeY:    sizeY,
		sizeZ:    sizeZ,
		cells:    make([]explosionCollisionCell, sizeX*sizeY*sizeZ),
		boxes:    make([]cube.BBox, 0, 64),
	}
	source := &explosionSnapshotSource{snapshot: snapshot, complete: true}
	for y := minPos[1]; y <= maxPos[1]; y++ {
		for z := minPos[2]; z <= maxPos[2]; z++ {
			for x := minPos[0]; x <= maxPos[0]; x++ {
				pos := cube.Pos{x, y, z}
				index, _ := volume.index(pos)
				cell := &volume.cells[index]
				if suppressLiquids {
					if _, liquid := snapshot.Liquid(pos); liquid {
						cell.flags |= explosionCollisionLiquid
					}
				}

				block := snapshot.Block(pos)
				blockModel := block.Model()
				switch blockModel.(type) {
				case model.Empty:
					continue
				case model.Solid:
					cell.flags |= explosionCollisionFull
					continue
				}
				if _, custom := block.(world.CustomBlock); custom {
					return nil, false
				}

				source.complete = true
				boxes := blockModel.BBox(pos, source)
				if !source.complete || len(boxes) > math.MaxUint16 || len(volume.boxes) > math.MaxUint32-len(boxes) {
					return nil, false
				}
				cell.boxStart = uint32(len(volume.boxes))
				cell.boxCount = uint16(len(boxes))
				for _, box := range boxes {
					volume.boxes = append(volume.boxes, box.Translate(pos.Vec3()))
				}
			}
		}
	}
	return volume, true
}

func shouldCompileExplosionExposure(entities []world.Entity) bool {
	var samples int
	for _, entity := range entities {
		if _, explodable := entity.(ExplodableEntity); !explodable {
			continue
		}
		box := entity.H().Type().BBox(entity)
		diff := box.Max().Sub(box.Min()).Mul(2.0).Add(mgl64.Vec3{1, 1, 1})
		count := 1
		for axis := range 3 {
			if diff[axis] <= 0 || math.IsNaN(diff[axis]) {
				continue
			}
			if math.IsInf(diff[axis], 0) || diff[axis] >= explosionExposureSampleCutoff {
				return true
			}
			axisSamples := int(math.Floor(diff[axis])) + 1
			if count >= (explosionExposureSampleCutoff+axisSamples-1)/axisSamples {
				return true
			}
			count *= axisSamples
		}
		if samples >= explosionExposureSampleCutoff-count {
			return true
		}
		samples += count
	}
	return false
}

func explosionCollisionBounds(origin mgl64.Vec3, entities []world.Entity) (cube.Pos, cube.Pos, bool) {
	minimum, maximum := origin, origin
	found := false
	for _, entity := range entities {
		if _, explodable := entity.(ExplodableEntity); !explodable {
			continue
		}
		position := entity.Position()
		box := entity.H().Type().BBox(entity).Translate(position)
		boxMin, boxMax := box.Min(), box.Max()
		for axis := range 3 {
			minimum[axis] = min(minimum[axis], boxMin[axis])
			maximum[axis] = max(maximum[axis], boxMax[axis])
		}
		found = true
	}
	if !found {
		return cube.Pos{}, cube.Pos{}, false
	}

	var minPos, maxPos cube.Pos
	const coordinateLimit = float64(1<<34 - 2)
	for axis := range 3 {
		if math.IsNaN(minimum[axis]) || math.IsInf(minimum[axis], 0) ||
			math.IsNaN(maximum[axis]) || math.IsInf(maximum[axis], 0) ||
			minimum[axis] < -coordinateLimit || maximum[axis] > coordinateLimit {
			return cube.Pos{}, cube.Pos{}, false
		}
		minPos[axis] = int(math.Floor(minimum[axis])) - 1
		maxPos[axis] = int(math.Floor(maximum[axis])) + 1
	}
	return minPos, maxPos, true
}

func (volume *explosionCollisionVolume) covers(origin mgl64.Vec3, entity world.Entity) bool {
	minPos, maxPos, ok := explosionCollisionBounds(origin, []world.Entity{entity})
	if !ok {
		return false
	}
	volumeMax := cube.Pos{
		volume.min[0] + volume.sizeX - 1,
		volume.min[1] + volume.sizeY - 1,
		volume.min[2] + volume.sizeZ - 1,
	}
	return minPos[0] >= volume.min[0] && minPos[1] >= volume.min[1] && minPos[2] >= volume.min[2] &&
		maxPos[0] <= volumeMax[0] && maxPos[1] <= volumeMax[1] && maxPos[2] <= volumeMax[2]
}

func (volume *explosionCollisionVolume) index(pos cube.Pos) (uint32, bool) {
	x, y, z := pos[0]-volume.min[0], pos[1]-volume.min[1], pos[2]-volume.min[2]
	if x < 0 || x >= volume.sizeX || y < 0 || y >= volume.sizeY || z < 0 || z >= volume.sizeZ {
		return 0, false
	}
	return uint32((y*volume.sizeZ+z)*volume.sizeX + x), true
}

func (volume *explosionCollisionVolume) intersects(pos cube.Pos, start, end mgl64.Vec3, suppressLiquids bool) (collided, complete bool) {
	index, ok := volume.index(pos)
	if !ok {
		return false, false
	}
	cell := volume.cells[index]
	if suppressLiquids && cell.flags&explosionCollisionLiquid != 0 {
		return true, true
	}
	if cell.flags&explosionCollisionFull != 0 {
		blockMin := pos.Vec3()
		return explosionBBoxBoundaryIntersects(cube.Box(
			blockMin[0], blockMin[1], blockMin[2],
			blockMin[0]+1, blockMin[1]+1, blockMin[2]+1,
		), start, end), true
	}
	for _, box := range volume.boxes[cell.boxStart : cell.boxStart+uint32(cell.boxCount)] {
		if explosionBBoxBoundaryIntersects(box, start, end) {
			return true, true
		}
	}
	return false, true
}

func (volume *explosionCollisionVolume) rayIntersects(start, end mgl64.Vec3, suppressLiquids bool) (collided, complete bool) {
	direction := end.Sub(start)
	if mgl64.FloatEqual(direction.LenSqr(), 0) {
		panic("start and end points are the same, giving a zero direction vector")
	}
	direction = direction.Normalize()

	pos := cube.PosFromVec3(start)
	stepX, stepY, stepZ := explosionDirectionSign(direction[0]), explosionDirectionSign(direction[1]), explosionDirectionSign(direction[2])
	maxX, maxY, maxZ := explosionBoundary(start[0], direction[0]), explosionBoundary(start[1], direction[1]), explosionBoundary(start[2], direction[2])
	deltaX, deltaY, deltaZ := explosionSafeDivide(float64(stepX), direction[0]), explosionSafeDivide(float64(stepY), direction[1]), explosionSafeDivide(float64(stepZ), direction[2])
	distance := start.Sub(end).Len()

	for {
		if collided, complete = volume.intersects(pos, start, end, suppressLiquids); collided || !complete {
			return collided, complete
		}
		switch {
		case maxX < maxY && maxX < maxZ:
			if maxX > distance {
				return false, true
			}
			pos[0] += stepX
			maxX += deltaX
		case maxY < maxZ:
			if maxY > distance {
				return false, true
			}
			pos[1] += stepY
			maxY += deltaY
		default:
			if maxZ > distance {
				return false, true
			}
			pos[2] += stepZ
			maxZ += deltaZ
		}
	}
}

func explosionDirectionSign(value float64) int {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	default:
		return 0
	}
}

func explosionSafeDivide(dividend, divisor float64) float64 {
	if divisor == 0 {
		return 0
	}
	return dividend / divisor
}

func explosionBoundary(start, direction float64) float64 {
	if direction == 0 {
		return math.Inf(1)
	}
	if direction < 0 {
		start, direction = -start, -direction
		if math.Floor(start) == start {
			return 0
		}
	}
	return (1 - (start - math.Floor(start))) / direction
}

func explosionBBoxBoundaryIntersects(box cube.BBox, start, end mgl64.Vec3) bool {
	minimum, maximum := box.Min(), box.Max()
	return explosionLineCrossesBoxFace(box, start, end, 0, minimum[0]) ||
		explosionLineCrossesBoxFace(box, start, end, 0, maximum[0]) ||
		explosionLineCrossesBoxFace(box, start, end, 1, minimum[1]) ||
		explosionLineCrossesBoxFace(box, start, end, 1, maximum[1]) ||
		explosionLineCrossesBoxFace(box, start, end, 2, minimum[2]) ||
		explosionLineCrossesBoxFace(box, start, end, 2, maximum[2])
}

func explosionLineCrossesBoxFace(box cube.BBox, start, end mgl64.Vec3, axis int, plane float64) bool {
	if mgl64.FloatEqual(end[axis], start[axis]) {
		return false
	}
	factor := (plane - start[axis]) / (end[axis] - start[axis])
	if factor < 0 || factor > 1 {
		return false
	}
	point := mgl64.Vec3{
		start[0] + (end[0]-start[0])*factor,
		start[1] + (end[1]-start[1])*factor,
		start[2] + (end[2]-start[2])*factor,
	}
	point[axis] = plane
	switch axis {
	case 0:
		return box.Vec3WithinYZ(point)
	case 1:
		return box.Vec3WithinXZ(point)
	default:
		return box.Vec3WithinXY(point)
	}
}

func (source *explosionSnapshotSource) Block(pos cube.Pos) world.Block {
	if !source.snapshot.Contains(pos) {
		source.complete = false
	}
	return source.snapshot.Block(pos)
}

func newLiveExplosionCollisionCache(origin mgl64.Vec3, box cube.BBox) *liveExplosionCollisionCache {
	boxMin, boxMax := box.Min(), box.Max()
	minimum, maximum := origin, origin
	for axis := range 3 {
		minimum[axis] = min(minimum[axis], boxMin[axis])
		maximum[axis] = max(maximum[axis], boxMax[axis])
	}
	var minPos, maxPos cube.Pos
	const coordinateLimit = float64(1<<34 - 2)
	for axis := range 3 {
		if math.IsNaN(minimum[axis]) || math.IsInf(minimum[axis], 0) ||
			math.IsNaN(maximum[axis]) || math.IsInf(maximum[axis], 0) ||
			minimum[axis] < -coordinateLimit || maximum[axis] > coordinateLimit {
			return nil
		}
		minPos[axis] = int(math.Floor(minimum[axis])) - 1
		maxPos[axis] = int(math.Floor(maximum[axis])) + 1
	}
	sizeX, sizeY, sizeZ := maxPos[0]-minPos[0]+1, maxPos[1]-minPos[1]+1, maxPos[2]-minPos[2]+1
	if sizeX <= 0 || sizeY <= 0 || sizeZ <= 0 || sizeX > 32768/sizeY || sizeX*sizeY > 32768/sizeZ {
		return nil
	}
	return &liveExplosionCollisionCache{
		min:   minPos,
		sizeX: sizeX,
		sizeY: sizeY,
		sizeZ: sizeZ,
		cells: make([]explosionCollisionCell, sizeX*sizeY*sizeZ),
		boxes: make([]cube.BBox, 0, 8),
	}
}

func (cache *liveExplosionCollisionCache) intersects(tx *world.Tx, pos cube.Pos, start, end mgl64.Vec3, suppressLiquids bool) bool {
	if cache == nil {
		return liveExplosionBlockIntersects(tx, pos, start, end, suppressLiquids)
	}
	index, ok := cache.index(pos)
	if !ok {
		return liveExplosionBlockIntersects(tx, pos, start, end, suppressLiquids)
	}
	cell := &cache.cells[index]
	if cell.flags&explosionCollisionCached == 0 {
		cell.flags |= explosionCollisionCached
		if suppressLiquids {
			if _, liquid := tx.Liquid(pos); liquid {
				cell.flags |= explosionCollisionLiquid
				return true
			}
		}
		blockModel := tx.Block(pos).Model()
		switch blockModel.(type) {
		case model.Empty:
		case model.Solid:
			cell.flags |= explosionCollisionFull
		default:
			boxes := blockModel.BBox(pos, tx)
			if len(boxes) > math.MaxUint16 || len(cache.boxes) > math.MaxUint32-len(boxes) {
				cell.flags &^= explosionCollisionCached
				return liveExplosionBlockIntersects(tx, pos, start, end, suppressLiquids)
			}
			cell.boxStart = uint32(len(cache.boxes))
			cell.boxCount = uint16(len(boxes))
			for _, box := range boxes {
				cache.boxes = append(cache.boxes, box.Translate(pos.Vec3()))
			}
		}
	}
	if cell.flags&explosionCollisionLiquid != 0 {
		return true
	}
	if cell.flags&explosionCollisionFull != 0 {
		blockMin := pos.Vec3()
		return explosionBBoxBoundaryIntersects(cube.Box(
			blockMin[0], blockMin[1], blockMin[2],
			blockMin[0]+1, blockMin[1]+1, blockMin[2]+1,
		), start, end)
	}
	for _, box := range cache.boxes[cell.boxStart : cell.boxStart+uint32(cell.boxCount)] {
		if explosionBBoxBoundaryIntersects(box, start, end) {
			return true
		}
	}
	return false
}

func (cache *liveExplosionCollisionCache) index(pos cube.Pos) (uint32, bool) {
	x, y, z := pos[0]-cache.min[0], pos[1]-cache.min[1], pos[2]-cache.min[2]
	if x < 0 || x >= cache.sizeX || y < 0 || y >= cache.sizeY || z < 0 || z >= cache.sizeZ {
		return 0, false
	}
	return uint32((y*cache.sizeZ+z)*cache.sizeX + x), true
}

func liveExplosionBlockIntersects(tx *world.Tx, pos cube.Pos, start, end mgl64.Vec3, suppressLiquids bool) bool {
	if suppressLiquids {
		if _, liquid := tx.Liquid(pos); liquid {
			return true
		}
	}
	block := tx.Block(pos)
	for _, box := range block.Model().BBox(pos, tx) {
		if explosionBBoxBoundaryIntersects(box.Translate(pos.Vec3()), start, end) {
			return true
		}
	}
	return false
}

func (c ExplosionConfig) compiledExposure(volume *explosionCollisionVolume, origin mgl64.Vec3, entity world.Entity) (float64, bool) {
	position := entity.Position()
	box := entity.H().Type().BBox(entity).Translate(position)
	boxMin, boxMax := box.Min(), box.Max()
	diff := boxMax.Sub(boxMin).Mul(2.0).Add(mgl64.Vec3{1, 1, 1})
	step := mgl64.Vec3{1.0 / diff[0], 1.0 / diff[1], 1.0 / diff[2]}
	if step[0] < 0.0 || step[1] < 0.0 || step[2] < 0.0 {
		return 0.0, true
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
				collided, complete := volume.rayIntersects(origin, point, c.SuppressUnderwaterImpact)
				if !complete {
					return 0, false
				}
				if !collided {
					misses++
				}
				checks++
			}
		}
	}
	return misses / checks, true
}
