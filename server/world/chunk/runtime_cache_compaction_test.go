package chunk

import (
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestCompactForRuntimeCacheEmptyChunk(t *testing.T) {
	chunk := New(networkHashTestRegistry{}, cube.Range{0, 15})

	chunk.CompactForRuntimeCache()

	if got := chunk.HighestFilledSubChunk(); got != 0 {
		t.Fatalf("highest filled subchunk = %d, want 0", got)
	}
}

func TestCompactForRuntimeCacheCollapsesSingleValueStorage(t *testing.T) {
	storage := testPalettedStorage(4, []uint32{testKnownRuntimeID})
	before := snapshotStorage(storage)

	storage.compactForRuntimeCache()

	assertStorageValues(t, storage, before)
	if storage.bitsPerIndex != 0 || storage.palette.size != 0 || len(storage.indices) != 0 {
		t.Fatalf("single-value storage not collapsed: bits=%d palette_size=%d indices=%d", storage.bitsPerIndex, storage.palette.size, len(storage.indices))
	}
}

func TestCompactForRuntimeCacheCollapsesUniformPackedStorage(t *testing.T) {
	storage := testPalettedStorage(3, []uint32{testAirRuntimeID, testKnownRuntimeID, testSecondRuntimeID})
	fillStorageIndex(storage, 1)
	for index := range storage.indices {
		storage.indices[index] |= ^repeatedPaletteIndexWord(uint16(storage.indexMask), storage.bitsPerIndex, uint32BitSize/int(storage.bitsPerIndex))
	}
	before := snapshotStorage(storage)

	storage.compactForRuntimeCache()

	assertStorageValues(t, storage, before)
	if storage.bitsPerIndex != 0 || storage.palette.Len() != 1 || storage.palette.Value(0) != testKnownRuntimeID {
		t.Fatalf("uniform storage not collapsed to runtime ID %d", testKnownRuntimeID)
	}
}

func TestCompactForRuntimeCacheShrinksOversizedStorage(t *testing.T) {
	values := []uint32{10, 20, 30, 40, 50}
	storage := testPalettedStorage(8, values)
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				storage.setPaletteIndex(x, y, z, uint16((int(x)+int(y)+int(z))%len(values)))
			}
		}
	}
	before := snapshotStorage(storage)

	storage.compactForRuntimeCache()

	assertStorageValues(t, storage, before)
	if storage.bitsPerIndex != 3 || storage.palette.size != 3 {
		t.Fatalf("storage width = %d/%d, want 3/3", storage.bitsPerIndex, storage.palette.size)
	}
	if !slices.Equal(storage.palette.values, values) {
		t.Fatalf("palette = %v, want %v", storage.palette.values, values)
	}
}

func TestUniformPaletteIndexAcrossStorageWidths(t *testing.T) {
	for _, bits := range []paletteSize{1, 2, 3, 4, 5, 6, 8, 16} {
		t.Run(fmt.Sprintf("%d_bit", bits), func(t *testing.T) {
			storage := testPalettedStorage(bits, []uint32{10, 20})
			fillStorageIndex(storage, 1)
			index, uniform := storage.uniformPaletteIndex()
			if !uniform || index != 1 {
				t.Fatalf("uniform index = %d, %t; want 1, true", index, uniform)
			}
			storage.setPaletteIndex(0, 0, 0, 0)
			if _, uniform := storage.uniformPaletteIndex(); uniform {
				t.Fatal("non-uniform storage reported as uniform")
			}
		})
	}
}

func TestCompactForRuntimeCachePreservesLayerIndices(t *testing.T) {
	sub := NewSubChunk(testAirRuntimeID)
	sub.SetBlock(1, 1, 1, 0, testKnownRuntimeID)
	sub.Layer(1)
	sub.SetBlock(2, 2, 2, 2, testSecondRuntimeID)
	before := snapshotSubChunk(sub, len(sub.storages))

	sub.compactForRuntimeCache()

	if len(sub.storages) != 3 {
		t.Fatalf("layers = %d, want interior air layer preserved", len(sub.storages))
	}
	assertSubChunkValues(t, sub, before)
}

func TestCompactForRuntimeCacheTrimsOnlyTrailingAirLayers(t *testing.T) {
	sub := NewSubChunk(testAirRuntimeID)
	sub.SetBlock(1, 1, 1, 0, testKnownRuntimeID)
	sub.Layer(2)
	before := snapshotSubChunk(sub, len(sub.storages))

	sub.compactForRuntimeCache()

	if len(sub.storages) != 1 {
		t.Fatalf("layers = %d, want one meaningful layer", len(sub.storages))
	}
	assertSubChunkValues(t, sub, before)
}

func TestChunkCompactForRuntimeCacheLeavesBiomesUntouched(t *testing.T) {
	chunk := New(networkHashTestRegistry{}, cube.Range{0, 15})
	chunk.SetBlock(1, 1, 1, 0, testKnownRuntimeID)
	chunk.SetBiome(2, 3, 4, testSecondRuntimeID)
	before := snapshotStorage(chunk.biomes[0])

	chunk.CompactForRuntimeCache()

	assertStorageValues(t, chunk.biomes[0], before)
	var nilChunk *Chunk
	nilChunk.CompactForRuntimeCache()
}

func testPalettedStorage(size paletteSize, values []uint32) *PalettedStorage {
	return newPalettedStorage(make([]uint32, size.uint32s()), newPalette(size, slices.Clone(values)))
}

func fillStorageIndex(storage *PalettedStorage, index uint16) {
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				storage.setPaletteIndex(x, y, z, index)
			}
		}
	}
}

func snapshotStorage(storage *PalettedStorage) []uint32 {
	values := make([]uint32, 0, 4096)
	for x := byte(0); x < 16; x++ {
		for y := byte(0); y < 16; y++ {
			for z := byte(0); z < 16; z++ {
				values = append(values, storage.At(x, y, z))
			}
		}
	}
	return values
}

func assertStorageValues(t *testing.T, storage *PalettedStorage, want []uint32) {
	t.Helper()
	if got := snapshotStorage(storage); !slices.Equal(got, want) {
		t.Fatal("storage values changed during runtime-cache compaction")
	}
}

func snapshotSubChunk(sub *SubChunk, layers int) [][]uint32 {
	values := make([][]uint32, layers)
	for layer := range layers {
		values[layer] = make([]uint32, 0, 4096)
		for x := byte(0); x < 16; x++ {
			for y := byte(0); y < 16; y++ {
				for z := byte(0); z < 16; z++ {
					values[layer] = append(values[layer], sub.Block(x, y, z, uint8(layer)))
				}
			}
		}
	}
	return values
}

func assertSubChunkValues(t *testing.T, sub *SubChunk, want [][]uint32) {
	t.Helper()
	got := snapshotSubChunk(sub, len(want))
	if len(got) != len(want) {
		t.Fatalf("subchunk layer count = %d, want %d", len(got), len(want))
	}
	for layer := range want {
		if !slices.Equal(got[layer], want[layer]) {
			t.Fatalf("subchunk layer %d changed during runtime-cache compaction", layer)
		}
	}
}
