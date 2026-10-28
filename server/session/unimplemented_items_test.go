package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestUnimplementedNativeItemsNetworkRoundTrip(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, name := range []string{"minecraft:red_mushroom", "minecraft:brown_mushroom_block", "minecraft:cave_vines_head_with_berries"} {
		var state world.Block
		for _, b := range world.Blocks() {
			if n, _ := b.EncodeBlock(); n == name {
				state = b
				break
			}
		}
		if state == nil {
			t.Fatalf("missing state %s", name)
		}
		i, ok := block.UnimplementedBlockItem(state)
		if !ok {
			t.Fatalf("missing native item for %s", name)
		}
		want := item.NewStack(i, 3).WithCustomName("Native drop").WithValue("test", "fallback")
		wire := stackFromItem(world.DefaultBlockRegistry, want)
		for _, blockID := range []int32{0, int32(world.BlockRuntimeID(state))} {
			wire.BlockRuntimeID = blockID
			got := stackToItem(world.DefaultBlockRegistry, wire)
			if !want.Equal(got) {
				t.Fatalf("%s blockID=%d: native item became %v", name, blockID, got)
			}
		}
		wire.NetworkID, _, _ = world.ItemRuntimeID(item.Diamond{})
		if got := stackToItem(world.DefaultBlockRegistry, wire); !got.Empty() {
			t.Fatalf("mismatched native item/block IDs were accepted: %v", got)
		}
	}
}
