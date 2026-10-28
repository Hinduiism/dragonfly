package block

import (
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// BreakInfoFor returns the implemented break rules, or basic player-mining rules for an unimplemented
// vanilla block. It does not make implemented unbreakable blocks breakable or add ticking/interactions.
func BreakInfoFor(b world.Block) (BreakInfo, bool) {
	if breakable, ok := b.(Breakable); ok {
		return breakable.BreakInfo(), true
	}
	if !world.IsUnimplementedBlock(b) {
		return BreakInfo{}, false
	}
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:command_block", "minecraft:chain_command_block", "minecraft:repeating_command_block",
		"minecraft:structure_block", "minecraft:structure_void", "minecraft:jigsaw", "minecraft:allow",
		"minecraft:deny", "minecraft:border_block", "minecraft:light_block", "minecraft:portal",
		"minecraft:end_portal", "minecraft:end_portal_frame", "minecraft:bedrock", "minecraft:invisible_bedrock", "minecraft:barrier":
		return BreakInfo{}, false
	}
	hardness := 1.0
	switch name {
	case "minecraft:brown_mushroom", "minecraft:red_mushroom", "minecraft:hanging_roots",
		"minecraft:cave_vines", "minecraft:cave_vines_head_with_berries", "minecraft:cave_vines_body_with_berries",
		"minecraft:weeping_vines", "minecraft:weeping_vines_plant", "minecraft:twisting_vines", "minecraft:twisting_vines_plant":
		hardness = 0
	}
	return newBreakInfo(hardness, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if drop, ok := world.ItemByName(unimplementedDropName(name), 0); ok {
			return []item.Stack{item.NewStack(drop, 1)}
		}
		return nil // Technical states without a native inventory item cannot produce an invented item ID.
	}), true
}

func unimplementedDropName(name string) string {
	switch name {
	case "minecraft:cave_vines", "minecraft:cave_vines_head_with_berries", "minecraft:cave_vines_body_with_berries":
		return "minecraft:glow_berries"
	case "minecraft:weeping_vines_plant":
		return "minecraft:weeping_vines"
	case "minecraft:twisting_vines_plant":
		return "minecraft:twisting_vines"
	}
	return name
}

// UnimplementedBlockItem resolves the native inventory item for an unsupported block state.
// It is also used when clients include the state's block runtime ID in inventory transactions.
func UnimplementedBlockItem(b world.Block) (world.Item, bool) {
	if !world.IsUnimplementedBlock(b) {
		return nil, false
	}
	name, _ := b.EncodeBlock()
	return world.ItemByName(unimplementedDropName(name), 0)
}

type nativeBlockDrop string

func (n nativeBlockDrop) EncodeItem() (string, int16) { return string(n), 0 }

func registerUnimplementedDrops() {
	for _, b := range world.UnimplementedBlocks() {
		name, _ := b.EncodeBlock()
		name = unimplementedDropName(name)
		if !strings.HasPrefix(name, "minecraft:") {
			continue
		}
		drop := nativeBlockDrop(name)
		if _, _, ok := world.ItemRuntimeID(drop); ok {
			world.RegisterItemFallback(drop)
		}
	}
}
