package chunk

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

const (
	testAirRuntimeID    = uint32(0)
	testKnownRuntimeID  = uint32(42)
	testSecondRuntimeID = uint32(84)
	testUnknownValue    = uint32(777)
	testKnownHash       = uint32(0x10203040)
	testSecondHash      = uint32(0x50607080)
)

type networkHashTestRegistry struct{}

func (networkHashTestRegistry) BlockCount() int { return 128 }
func (networkHashTestRegistry) AirRuntimeID() uint32 {
	return testAirRuntimeID
}
func (networkHashTestRegistry) RuntimeIDToState(uint32) (string, map[string]any, bool) {
	return "", nil, false
}
func (networkHashTestRegistry) StateToRuntimeID(string, map[string]any) (uint32, bool) {
	return 0, false
}
func (networkHashTestRegistry) FilteringBlock(uint32) uint8 { return 0 }
func (networkHashTestRegistry) LightBlock(uint32) uint8     { return 0 }
func (networkHashTestRegistry) RandomTickBlock(uint32) bool { return false }
func (networkHashTestRegistry) NBTBlock(uint32) bool        { return false }
func (networkHashTestRegistry) LiquidDisplacingBlock(uint32) bool {
	return false
}
func (networkHashTestRegistry) LiquidBlock(uint32) bool { return false }
func (networkHashTestRegistry) HashToRuntimeID(hash uint32) (uint32, bool) {
	switch hash {
	case testKnownHash:
		return testKnownRuntimeID, true
	case testSecondHash:
		return testSecondRuntimeID, true
	default:
		return 0, false
	}
}
func (networkHashTestRegistry) RuntimeIDToHash(runtimeID uint32) (uint32, bool) {
	switch runtimeID {
	case testKnownRuntimeID:
		return testKnownHash, true
	case testSecondRuntimeID:
		return testSecondHash, true
	default:
		return 0, false
	}
}

func TestConvertBlockNetworkHashesToRuntimeIDs(t *testing.T) {
	registry := networkHashTestRegistry{}
	chunk := New(registry, cube.Range{0, 15})
	chunk.SetBlock(1, 2, 3, 0, testKnownHash)
	chunk.SetBlock(4, 5, 6, 0, testUnknownValue)
	chunk.SetBlock(7, 8, 9, 1, testSecondHash)
	chunk.SetBiome(1, 2, 3, testKnownHash)

	if index := chunk.sub[0].storages[0].palette.Index(testKnownHash); index < 0 {
		t.Fatal("known hash missing from source palette")
	}
	chunk.ConvertBlockNetworkHashesToRuntimeIDs()

	assertChunkBlock(t, chunk, 1, 2, 3, 0, testKnownRuntimeID)
	assertChunkBlock(t, chunk, 4, 5, 6, 0, testUnknownValue)
	assertChunkBlock(t, chunk, 7, 8, 9, 1, testSecondRuntimeID)
	if got := chunk.Biome(1, 2, 3); got != testKnownHash {
		t.Fatalf("biome = %d, want unchanged value %d", got, testKnownHash)
	}
	palette := chunk.sub[0].storages[0].palette
	if index := palette.Index(testKnownRuntimeID); index < 0 {
		t.Fatal("converted runtime ID missing from palette")
	}
	if index := palette.Index(testKnownHash); index != -1 {
		t.Fatalf("stale hash still present at palette index %d", index)
	}
}

func TestConvertBlockNetworkHashesNilInputs(t *testing.T) {
	var nilChunk *Chunk
	nilChunk.ConvertBlockNetworkHashesToRuntimeIDs()

	var nilSubChunk *SubChunk
	nilSubChunk.ConvertBlockNetworkHashesToRuntimeIDs(networkHashTestRegistry{})

	sub := &SubChunk{storages: []*PalettedStorage{nil, {}}}
	sub.ConvertBlockNetworkHashesToRuntimeIDs(nil)
	sub.ConvertBlockNetworkHashesToRuntimeIDs(networkHashTestRegistry{})
}

func TestEncodeWithBlockNetworkHashes(t *testing.T) {
	registry := networkHashTestRegistry{}
	chunkRange := cube.Range{0, 15}
	chunk := New(registry, chunkRange)
	chunk.SetBlock(1, 2, 3, 0, testKnownRuntimeID)
	chunk.SetBlock(4, 5, 6, 0, testUnknownValue)
	chunk.SetBlock(7, 8, 9, 1, testSecondRuntimeID)
	chunk.SetBiome(1, 2, 3, testKnownRuntimeID)

	serialised := EncodeWithBlockNetworkHashes(chunk)
	payload := bytes.Join(append(serialised.SubChunks, serialised.Biomes), nil)
	decoded, err := NetworkDecode(registry, payload, len(serialised.SubChunks), chunkRange)
	if err != nil {
		t.Fatalf("decode hash-encoded chunk: %v", err)
	}

	assertChunkBlock(t, decoded, 1, 2, 3, 0, testKnownHash)
	assertChunkBlock(t, decoded, 4, 5, 6, 0, testUnknownValue)
	assertChunkBlock(t, decoded, 7, 8, 9, 1, testSecondHash)
	if got := decoded.Biome(1, 2, 3); got != testKnownRuntimeID {
		t.Fatalf("encoded biome = %d, want unchanged value %d", got, testKnownRuntimeID)
	}

	assertChunkBlock(t, chunk, 1, 2, 3, 0, testKnownRuntimeID)
	assertChunkBlock(t, chunk, 4, 5, 6, 0, testUnknownValue)
	assertChunkBlock(t, chunk, 7, 8, 9, 1, testSecondRuntimeID)
	if got := chunk.Biome(1, 2, 3); got != testKnownRuntimeID {
		t.Fatalf("source biome = %d, want %d", got, testKnownRuntimeID)
	}
}

func TestEncodeWithBlockNetworkHashesNilChunk(t *testing.T) {
	serialised := EncodeWithBlockNetworkHashes(nil)
	if serialised.SubChunks != nil || serialised.Biomes != nil {
		t.Fatalf("nil chunk encoded as %#v, want empty serialised data", serialised)
	}
}

func assertChunkBlock(t *testing.T, chunk *Chunk, x uint8, y int16, z, layer uint8, want uint32) {
	t.Helper()
	if got := chunk.Block(x, y, z, layer); got != want {
		t.Fatalf("block (%d,%d,%d) layer %d = %d, want %d", x, y, z, layer, got, want)
	}
}
