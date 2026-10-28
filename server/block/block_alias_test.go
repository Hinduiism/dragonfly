package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestPowderSnowAliasCoversDiskWorldAndClientPalettes(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	// A vanilla registry represents the native client and pre-existing world files.
	vanilla := world.NewBlockRegistry().(*world.BasicBlockRegistry)
	vanilla.UnregisterBlockAlias(world.BlockState{Name: "minecraft:powder_snow"})
	vanilla.Finalize()
	native, ok := vanilla.BlockByName("minecraft:powder_snow", nil)
	if !ok {
		t.Fatal("powder-snow state missing")
	}
	raw := vanilla.BlockRuntimeID(native)
	registry := world.NewBlockRegistry().(*world.BasicBlockRegistry)
	registry.Finalize()
	for _, reg := range []*world.BasicBlockRegistry{world.DefaultBlockRegistry, registry, registry.Clone()} {
		want := reg.BlockRuntimeID(block.Snow{})
		if got := reg.BlockRuntimeID(native); got != want {
			t.Fatalf("world placement encoded powder snow: %d", got)
		}
		if got, _ := reg.StateToRuntimeID("minecraft:powder_snow", nil); got != want {
			t.Fatal("disk state was not normalized")
		}
		if name, _, _ := reg.RuntimeIDToState(raw); name != "minecraft:snow" {
			t.Fatal("disk save retained powder snow")
		}
		if b, _ := reg.BlockByRuntimeID(raw); b != (block.Snow{}) {
			t.Fatalf("server collision retained %T", b)
		}
		if name, _ := func() (string, map[string]any) {
			b, _ := reg.BlockByName("minecraft:powder_snow", nil)
			return b.EncodeBlock()
		}(); name != "minecraft:snow" {
			t.Fatal("name lookup retained powder snow")
		}
		if h, _ := reg.RuntimeIDToHash(raw); h != func() uint32 { h, _ := reg.RuntimeIDToHash(want); return h }() {
			t.Fatal("network hash retained powder snow")
		}
		originalHash, _ := vanilla.RuntimeIDToHash(raw)
		if got, _ := reg.HashToRuntimeID(originalHash); got != want {
			t.Fatal("native input hash bypassed the alias")
		}

		// A native on-disk chunk must load as real snow.
		r := cube.Range{0, 15}
		original := chunk.New(vanilla, r)
		original.SetBlock(1, 1, 1, 0, raw)
		loaded, err := chunk.DiskDecode(reg, chunk.Encode(original, chunk.DiskEncoding), r)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Block(1, 1, 1, 0) != want {
			t.Fatal("loaded chunk retained powder snow")
		}
		// Generators that supply a raw native runtime ID are normalized too.
		loaded.SetBlock(2, 1, 1, 0, raw)
		if loaded.Block(2, 1, 1, 0) != want {
			t.Fatal("generator write bypassed normalization")
		}
		for _, hashes := range []bool{false, true} {
			data := chunk.Encode(loaded, chunk.NetworkEncoding)
			if hashes {
				data = chunk.EncodeWithBlockNetworkHashes(loaded)
			}
			var payload []byte
			for _, sub := range data.SubChunks {
				payload = append(payload, sub...)
			}
			payload = append(payload, data.Biomes...)
			client, err := chunk.NetworkDecode(vanilla, payload, len(data.SubChunks), r)
			if err != nil {
				t.Fatal(err)
			}
			if hashes {
				client.ConvertBlockNetworkHashesToRuntimeIDs()
			}
			if got := client.Block(1, 1, 1, 0); got != world.BlockRuntimeID(block.Snow{}) {
				t.Fatalf("client received powder snow (hashes=%v): %d", hashes, got)
			}
		}
	}
	if b, _ := world.BlockByName("minecraft:powder_snow", nil); b != (block.Snow{}) {
		t.Fatal("fork default did not convert powder snow without application configuration")
	}
}
