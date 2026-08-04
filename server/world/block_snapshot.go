package world

import "github.com/df-mc/dragonfly/server/block/cube"

const maxBlockSnapshotCells = 1 << 20

// BlockSnapshot is an immutable copy of the block states in a bounded region.
// It is intended for calculations performed during the transaction that
// created it and must not be retained by application code.
type BlockSnapshot struct {
	tx       *Tx
	revision uint64
	registry BlockRegistry

	min, max            cube.Pos
	sizeX, sizeY, sizeZ int
	primary             []uint32
	liquidLayer         []uint32
	blockEntities       map[int]Block
}

// SnapshotBlocks copies block state in the inclusive bounds without loading
// or generating chunks. It returns false if any required in-range chunk is not
// already loaded or if the state cannot be represented safely.
func (tx *Tx) SnapshotBlocks(min, max cube.Pos) (*BlockSnapshot, bool) {
	w := tx.World()
	sizeX, okX := snapshotSpan(min[0], max[0])
	sizeY, okY := snapshotSpan(min[1], max[1])
	sizeZ, okZ := snapshotSpan(min[2], max[2])
	if !okX || !okY || !okZ || !snapshotCellCountValid(sizeX, sizeY, sizeZ) {
		return nil, false
	}
	if !snapshotChunkCoordinateValid(min[0]) || !snapshotChunkCoordinateValid(max[0]) ||
		!snapshotChunkCoordinateValid(min[2]) || !snapshotChunkCoordinateValid(max[2]) {
		return nil, false
	}

	registry := w.conf.Blocks
	air := registry.AirRuntimeID()
	cellCount := sizeX * sizeY * sizeZ
	primary := make([]uint32, cellCount)
	liquidLayer := make([]uint32, cellCount)
	if air != 0 {
		for i := range primary {
			primary[i] = air
			liquidLayer[i] = air
		}
	}

	snapshot := &BlockSnapshot{
		tx:          tx,
		revision:    tx.blockRevision,
		registry:    registry,
		min:         min,
		max:         max,
		sizeX:       sizeX,
		sizeY:       sizeY,
		sizeZ:       sizeZ,
		primary:     primary,
		liquidLayer: liquidLayer,
	}

	worldMinY, worldMaxY := w.Range().Min(), w.Range().Max()
	copyMinY, copyMaxY := maxInt(min[1], worldMinY), minInt(max[1], worldMaxY)
	if copyMinY > copyMaxY {
		return snapshot, true
	}

	minChunkX, maxChunkX := min[0]>>4, max[0]>>4
	minChunkZ, maxChunkZ := min[2]>>4, max[2]>>4
	for chunkX := minChunkX; chunkX <= maxChunkX; chunkX++ {
		for chunkZ := minChunkZ; chunkZ <= maxChunkZ; chunkZ++ {
			column, ok := w.loadedChunk(ChunkPos{int32(chunkX), int32(chunkZ)})
			if !ok {
				return nil, false
			}

			copyMinX, copyMaxX := maxInt(min[0], chunkX<<4), minInt(max[0], (chunkX<<4)+15)
			copyMinZ, copyMaxZ := maxInt(min[2], chunkZ<<4), minInt(max[2], (chunkZ<<4)+15)
			for y := copyMinY; y <= copyMaxY; y++ {
				for z := copyMinZ; z <= copyMaxZ; z++ {
					for x := copyMinX; x <= copyMaxX; x++ {
						pos := cube.Pos{x, y, z}
						index := snapshot.index(pos)
						primaryRID := column.Block(uint8(x), int16(y), uint8(z), 0)
						snapshot.primary[index] = primaryRID
						snapshot.liquidLayer[index] = column.Block(uint8(x), int16(y), uint8(z), 1)

						if registry.NBTBlock(primaryRID) {
							blockEntity, ok := column.BlockEntities[pos]
							if !ok {
								return nil, false
							}
							if snapshot.blockEntities == nil {
								snapshot.blockEntities = make(map[int]Block)
							}
							snapshot.blockEntities[index] = blockEntity
						}
					}
				}
			}
		}
	}
	return snapshot, true
}

// Bounds returns the inclusive minimum and maximum positions copied into the
// snapshot.
func (s *BlockSnapshot) Bounds() (min, max cube.Pos) {
	return s.min, s.max
}

// Contains reports whether pos falls inside the snapshot's inclusive bounds.
func (s *BlockSnapshot) Contains(pos cube.Pos) bool {
	return pos[0] >= s.min[0] && pos[0] <= s.max[0] &&
		pos[1] >= s.min[1] && pos[1] <= s.max[1] &&
		pos[2] >= s.min[2] && pos[2] <= s.max[2]
}

// Block returns the primary block at pos. Positions outside the snapshot are
// represented as air.
func (s *BlockSnapshot) Block(pos cube.Pos) Block {
	if !s.Contains(pos) {
		return s.registry.Air()
	}
	index := s.index(pos)
	if blockEntity, ok := s.blockEntities[index]; ok {
		return blockEntity
	}
	return s.registry.BlockByRuntimeIDOrAir(s.primary[index])
}

// Liquid returns the liquid at pos, preferring a liquid in the primary layer
// over one in the additional liquid layer.
func (s *BlockSnapshot) Liquid(pos cube.Pos) (Liquid, bool) {
	if !s.Contains(pos) {
		return nil, false
	}
	index := s.index(pos)
	if liquid, ok := s.registry.BlockByRuntimeIDOrAir(s.primary[index]).(Liquid); ok {
		return liquid, true
	}
	liquid, ok := s.registry.BlockByRuntimeIDOrAir(s.liquidLayer[index]).(Liquid)
	return liquid, ok
}

// Current reports whether no block mutation has occurred in tx since the
// snapshot was captured.
func (s *BlockSnapshot) Current(tx *Tx) bool {
	return s != nil && tx != nil && !tx.closed && s.tx == tx && s.revision == tx.blockRevision
}

func (s *BlockSnapshot) index(pos cube.Pos) int {
	return ((pos[1]-s.min[1])*s.sizeZ+(pos[2]-s.min[2]))*s.sizeX + pos[0] - s.min[0]
}

func snapshotSpan(minimum, maximum int) (int, bool) {
	if minimum > maximum {
		return 0, false
	}
	span := uint(maximum) - uint(minimum) + 1
	if span == 0 || span > uint(maxBlockSnapshotCells) {
		return 0, false
	}
	return int(span), true
}

func snapshotCellCountValid(sizeX, sizeY, sizeZ int) bool {
	return sizeX <= maxBlockSnapshotCells/sizeY && sizeX*sizeY <= maxBlockSnapshotCells/sizeZ
}

func snapshotChunkCoordinateValid(coordinate int) bool {
	chunkCoordinate := coordinate >> 4
	return chunkCoordinate >= -1<<31 && chunkCoordinate <= 1<<31-1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
