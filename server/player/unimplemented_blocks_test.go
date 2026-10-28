package player_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func TestUnimplementedBlocksBreakAndDropNativeItems(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	wanted := map[string]string{
		"minecraft:brown_mushroom": "minecraft:brown_mushroom", "minecraft:red_mushroom": "minecraft:red_mushroom",
		"minecraft:brown_mushroom_block": "minecraft:brown_mushroom_block", "minecraft:red_mushroom_block": "minecraft:red_mushroom_block",
		"minecraft:hanging_roots": "minecraft:hanging_roots", "minecraft:weeping_vines": "minecraft:weeping_vines",
		"minecraft:twisting_vines": "minecraft:twisting_vines", "minecraft:cave_vines": "minecraft:glow_berries",
		"minecraft:cave_vines_head_with_berries": "minecraft:glow_berries", "minecraft:cave_vines_body_with_berries": "minecraft:glow_berries",
		"minecraft:snow_layer": "minecraft:snow_layer", "minecraft:bell": "minecraft:bell",
	}
	seen := map[string]bool{}
	for index, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		want, ok := wanted[name]
		if !ok {
			continue
		}
		seen[name] = true
		t.Run(fmt.Sprintf("%s/%d", name, index), func(t *testing.T) {
			withBlockBreakDropsPlayer(t, world.GameModeSurvival, func(p *player.Player) {
				if d := block.BreakDuration(b, item.Stack{}, block.BreakContext{}); d < 0 || d > time.Minute {
					t.Fatalf("fallback mining duration is not finite: %v", d)
				}
				pos := cube.Pos{1, 64, 0}
				p.Tx().SetBlock(pos, b, nil)
				p.StartBreaking(pos, cube.FaceWest)
				p.FinishBreaking()
				p.BreakBlock(pos) // Repeated completion must not duplicate drops.
				assertBreakAir(t, p, pos)
				drops, xp := breakGround(p.Tx())
				if len(drops) != 1 || drops[0].Count() != 1 || xp != 0 {
					t.Fatalf("drops/XP = %v/%d, want one native item", drops, xp)
				}
				if got, _ := drops[0].Item().EncodeItem(); got != want {
					t.Fatalf("drop = %s, want %s", got, want)
				}
				if _, _, native := world.ItemRuntimeID(drops[0].Item()); !native {
					t.Fatal("drop has no native inventory runtime ID")
				}
				roundTrip := item.ReadNBT(item.WriteNBT(drops[0], true), nil)
				if roundTrip.Empty() || !roundTrip.Equal(drops[0]) {
					t.Fatalf("native drop did not survive inventory persistence: %v", roundTrip)
				}
			})
		})
	}
	for name := range wanted {
		if !seen[name] {
			t.Errorf("test missed registered block %s", name)
		}
	}
}

func TestUnimplementedBlocksRespectProtection(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, test := range []struct {
		name   string
		mode   world.GameMode
		pos    cube.Pos
		cancel bool
	}{
		{"cancelled", world.GameModeSurvival, cube.Pos{1, 64, 0}, true},
		{"adventure", world.GameModeAdventure, cube.Pos{1, 64, 0}, false},
		{"out of reach", world.GameModeSurvival, cube.Pos{32, 64, 0}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			withBlockBreakDropsPlayer(t, test.mode, func(p *player.Player) {
				b, ok := world.BlockByName("minecraft:red_mushroom", nil)
				if !ok {
					t.Fatal("missing mushroom state")
				}
				p.Tx().SetBlock(test.pos, b, nil)
				h := &legacyBlockBreakHandler{breakFn: func(ctx *player.Context, _ cube.Pos, _ *[]item.Stack, _ *int) {
					if test.cancel {
						ctx.Cancel()
					}
				}}
				p.Handle(h)
				p.BreakBlock(test.pos)
				name, _ := p.Tx().Block(test.pos).EncodeBlock()
				if name != "minecraft:red_mushroom" {
					t.Fatal("protected block was removed")
				}
				if drops, xp := breakGround(p.Tx()); len(drops) != 0 || xp != 0 {
					t.Fatal("rejected break produced rewards")
				}
				if test.cancel && h.breakCalls != 1 {
					t.Fatal("fallback bypassed the protection callback")
				}
			})
		})
	}
	for _, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		if name == "minecraft:command_block" || name == "minecraft:bedrock" || name == "minecraft:end_portal_frame" {
			if _, ok := block.BreakInfoFor(b); ok {
				t.Errorf("%s became breakable in Survival", name)
			}
		}
	}
}
