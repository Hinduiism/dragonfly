package player_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBlockBreakDropsAccepted(t *testing.T) {
	for _, capacity := range []string{"empty", "partial", "full"} {
		t.Run(capacity, func(t *testing.T) {
			withBlockBreakDropsPlayer(t, world.GameModeSurvival, func(p *player.Player) {
				pos := cube.Pos{1, 64, 0}
				p.Tx().SetBlock(pos, block.Stone{}, nil)
				tool, left := p.HeldItems()
				before := p.Data()
				outputs := []item.Stack{
					item.NewStack(block.Cobblestone{}, 5).WithCustomName("Approved stone").WithLore("break drops").WithValue("source", "test"),
					item.NewStack(item.Diamond{}, 2),
				}
				wantInserted := []int{5, 2}
				if capacity != "empty" {
					for slot := 1; slot < p.Inventory().Size(); slot++ {
						if err := p.Inventory().SetItem(slot, item.NewStack(block.Dirt{}, 64)); err != nil {
							t.Fatal(err)
						}
					}
					wantInserted = []int{0, 0}
					if capacity == "partial" {
						wantInserted = []int{3, 1}
						for i, output := range outputs {
							if err := p.Inventory().SetItem(i+1, output.Grow(output.MaxCount()-wantInserted[i]-output.Count())); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				inventoryBefore := []int{stackCount(p.Inventory().Items(), outputs[0]), stackCount(p.Inventory().Items(), outputs[1])}
				h := &blockBreakDropsHandler{legacyBlockBreakHandler: legacyBlockBreakHandler{breakFn: func(_ *player.Context, _ cube.Pos, drops *[]item.Stack, xp *int) {
					if len(*drops) != 1 || !(*drops)[0].Equal(item.NewStack(block.Cobblestone{}, 1)) {
						t.Fatalf("native proposed drops = %v, want one cobblestone", *drops)
					}
					*drops = append([]item.Stack(nil), outputs...)
					*xp = 7
				}}}
				h.dropsFn = func(gotPlayer *player.Player, gotPos cube.Pos, broken world.Block, drops *[]item.Stack) {
					if gotPlayer != p || gotPos != pos {
						t.Fatalf("callback player/position = %p/%v, want %p/%v", gotPlayer, gotPos, p, pos)
					}
					if _, ok := broken.(block.Stone); !ok {
						t.Fatalf("broken block = %T, want block.Stone", broken)
					}
					assertBreakAir(t, p, pos)
					ground, xp := breakGround(p.Tx())
					if len(ground) != 0 || xp != 7 {
						t.Fatalf("at callback: ground = %v, XP = %d, want no ground drops and 7 XP", ground, xp)
					}
					assertBreakState(t, p, tool, left, before.Exhaustion, before.Experience)
					insertBreakDrops(p, drops)
				}
				p.Handle(h)
				p.BreakBlock(pos)
				p.BreakBlock(pos)
				if h.breakCalls != 1 || h.dropsCalls != 1 {
					t.Fatalf("pre/post calls = %d/%d, want 1/1 after repeated break", h.breakCalls, h.dropsCalls)
				}
				ground, xp := breakGround(p.Tx())
				if xp != 7 {
					t.Fatalf("ground XP = %d, want 7", xp)
				}
				wantGroundStacks := 0
				for i, output := range outputs {
					inserted := stackCount(p.Inventory().Items(), output) - inventoryBefore[i]
					remainder := stackCount(ground, output)
					if inserted != wantInserted[i] || remainder != output.Count()-inserted {
						t.Fatalf("output %d: inserted/ground = %d/%d, want %d/%d", i, inserted, remainder, wantInserted[i], output.Count()-wantInserted[i])
					}
					if remainder != 0 {
						wantGroundStacks++
					}
				}
				if len(ground) != wantGroundStacks {
					t.Fatalf("ground stacks = %v, want %d stacks", ground, wantGroundStacks)
				}
				assertBreakState(t, p, tool.Damage(1), left, before.Exhaustion+0.005, before.Experience)
			})
		})
	}
}

func TestBlockBreakDropsRejected(t *testing.T) {
	for _, tt := range []struct {
		name   string
		pos    cube.Pos
		b      world.Block
		mode   world.GameMode
		cancel bool
	}{
		{name: "cancelled", pos: cube.Pos{1, 64, 0}, b: block.Stone{}, mode: world.GameModeSurvival, cancel: true},
		{name: "out of reach", pos: cube.Pos{32, 64, 0}, b: block.Stone{}, mode: world.GameModeSurvival},
		{name: "air", pos: cube.Pos{1, 64, 0}, b: block.Air{}, mode: world.GameModeSurvival},
		{name: "adventure", pos: cube.Pos{1, 64, 0}, b: block.Stone{}, mode: world.GameModeAdventure},
		{name: "unbreakable", pos: cube.Pos{1, 64, 0}, b: block.Bedrock{}, mode: world.GameModeSurvival},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withBlockBreakDropsPlayer(t, tt.mode, func(p *player.Player) {
				p.Tx().SetBlock(tt.pos, tt.b, nil)
				tool, left := p.HeldItems()
				before := p.Data()
				h := &blockBreakDropsHandler{legacyBlockBreakHandler: legacyBlockBreakHandler{breakFn: func(ctx *player.Context, _ cube.Pos, _ *[]item.Stack, _ *int) {
					if tt.cancel {
						ctx.Cancel()
					}
				}}}
				p.Handle(h)
				p.BreakBlock(tt.pos)
				wantPre := 0
				if tt.cancel {
					wantPre = 1
				}
				if h.breakCalls != wantPre || h.dropsCalls != 0 || p.Tx().Block(tt.pos) != tt.b {
					t.Fatalf("pre/post/block = %d/%d/%v, want %d/0/%v", h.breakCalls, h.dropsCalls, p.Tx().Block(tt.pos), wantPre, tt.b)
				}
				ground, xp := breakGround(p.Tx())
				if len(ground) != 0 || xp != 0 || len(p.Inventory().Items()) != 1 {
					t.Fatalf("rejected break rewarded items/XP: ground = %v, XP = %d, inventory = %v", ground, xp, p.Inventory().Items())
				}
				assertBreakState(t, p, tool, left, before.Exhaustion, before.Experience)
			})
		})
	}
}

func TestBlockBreakDropsCreative(t *testing.T) {
	withBlockBreakDropsPlayer(t, world.GameModeCreative, func(p *player.Player) {
		pos := cube.Pos{1, 64, 0}
		p.Tx().SetBlock(pos, block.DiamondOre{}, nil)
		tool, left := p.HeldItems()
		before := p.Data()
		h := &blockBreakDropsHandler{dropsFn: func(p *player.Player, pos cube.Pos, _ world.Block, drops *[]item.Stack) {
			assertBreakAir(t, p, pos)
			if len(*drops) != 0 {
				t.Fatalf("creative drops = %v, want empty", *drops)
			}
			insertBreakDrops(p, drops)
		}}
		p.Handle(h)
		p.BreakBlock(pos)
		p.BreakBlock(pos)
		ground, xp := breakGround(p.Tx())
		if h.breakCalls != 1 || h.dropsCalls != 1 || len(ground) != 0 || xp != 0 || len(p.Inventory().Items()) != 1 {
			t.Fatalf("creative pre/post/ground/XP/inventory = %d/%d/%v/%d/%v", h.breakCalls, h.dropsCalls, ground, xp, p.Inventory().Items())
		}
		assertBreakState(t, p, tool, left, before.Exhaustion, before.Experience)
	})
}

func TestBlockBreakDropsLegacyHandlers(t *testing.T) {
	for _, name := range []string{"NopHandler", "legacy handler"} {
		t.Run(name, func(t *testing.T) {
			withBlockBreakDropsPlayer(t, world.GameModeSurvival, func(p *player.Player) {
				legacy := &legacyBlockBreakHandler{}
				var h player.Handler = player.NopHandler{}
				if name == "legacy handler" {
					h = legacy
				}
				if _, ok := h.(player.BlockBreakDropsHandler); ok {
					t.Fatal("existing handler unexpectedly implements optional interface")
				}
				p.Handle(h)
				pos := cube.Pos{1, 64, 0}
				p.Tx().SetBlock(pos, block.DiamondOre{}, nil)
				tool, left := p.HeldItems()
				before := p.Data()
				p.BreakBlock(pos)
				assertBreakAir(t, p, pos)
				ground, xp := breakGround(p.Tx())
				if len(ground) != 1 || !ground[0].Equal(item.NewStack(item.Diamond{}, 1)) || xp < 3 || xp > 7 || len(p.Inventory().Items()) != 1 {
					t.Fatalf("native ground/XP/inventory = %v/%d/%v", ground, xp, p.Inventory().Items())
				}
				if name == "legacy handler" && legacy.breakCalls != 1 {
					t.Fatalf("legacy pre-break calls = %d, want 1", legacy.breakCalls)
				}
				assertBreakState(t, p, tool.Damage(1), left, before.Exhaustion+0.005, before.Experience)
			})
		})
	}
}

func TestBlockBreakDropsFilledChest(t *testing.T) {
	withBlockBreakDropsPlayer(t, world.GameModeSurvival, func(p *player.Player) {
		pos := cube.Pos{1, 64, 0}
		chest := block.NewChest()
		contents := item.NewStack(item.Diamond{}, 5).WithCustomName("Stored diamonds")
		if err := chest.Inventory(p.Tx(), pos).SetItem(0, contents); err != nil {
			t.Fatal(err)
		}
		p.Tx().SetBlock(pos, chest, nil)
		tool, left := p.HeldItems()
		before := p.Data()
		h := &blockBreakDropsHandler{legacyBlockBreakHandler: legacyBlockBreakHandler{breakFn: func(_ *player.Context, _ cube.Pos, drops *[]item.Stack, _ *int) {
			if chest.Inventory(p.Tx(), pos).Empty() || len(*drops) != 1 {
				t.Fatal("pre-break must still see filled chest and its own drop")
			}
		}}}
		h.dropsFn = func(p *player.Player, pos cube.Pos, broken world.Block, drops *[]item.Stack) {
			assertBreakAir(t, p, pos)
			if c, ok := broken.(block.Chest); !ok || !c.Inventory(p.Tx(), pos).Empty() {
				t.Fatalf("post-break original chest is not drained: %T", broken)
			}
			if len(*drops) != 1 {
				t.Fatalf("post-break drops = %v, want only chest", *drops)
			}
			decoded := item.ReadNBT(item.WriteNBT((*drops)[0], true), nil)
			if c, ok := decoded.Item().(block.Chest); !ok || !c.Inventory(p.Tx(), pos).Empty() {
				t.Fatal("serialized chest drop retained contents")
			}
			ground, xp := breakGround(p.Tx())
			if len(ground) != 1 || !ground[0].Equal(contents) || xp != 0 {
				t.Fatalf("native contents before hook = %v, XP = %d", ground, xp)
			}
			insertBreakDrops(p, drops)
		}
		p.Handle(h)
		p.BreakBlock(pos)
		p.BreakBlock(pos)
		ground, xp := breakGround(p.Tx())
		if h.breakCalls != 1 || h.dropsCalls != 1 || len(ground) != 1 || !ground[0].Equal(contents) || xp != 0 {
			t.Fatalf("chest pre/post/ground/XP = %d/%d/%v/%d", h.breakCalls, h.dropsCalls, ground, xp)
		}
		stored, err := p.Inventory().Item(1)
		if err != nil || stored.Count() != 1 || len(p.Inventory().Items()) != 2 {
			t.Fatalf("inserted chest/inventory = %v/%v, error = %v", stored, p.Inventory().Items(), err)
		}
		decoded := item.ReadNBT(item.WriteNBT(stored, true), nil)
		if c, ok := decoded.Item().(block.Chest); !ok || !c.Inventory(p.Tx(), pos).Empty() {
			t.Fatal("serialized inventory chest retained contents")
		}
		assertBreakState(t, p, tool.Damage(1), left, before.Exhaustion+0.005, before.Experience)
	})
}

type legacyBlockBreakHandler struct {
	player.NopHandler
	breakCalls int
	breakFn    func(*player.Context, cube.Pos, *[]item.Stack, *int)
}

func (h *legacyBlockBreakHandler) HandleBlockBreak(ctx *player.Context, pos cube.Pos, drops *[]item.Stack, xp *int) {
	h.breakCalls++
	if h.breakFn != nil {
		h.breakFn(ctx, pos, drops, xp)
	}
}

type blockBreakDropsHandler struct {
	legacyBlockBreakHandler
	dropsCalls int
	dropsFn    func(*player.Player, cube.Pos, world.Block, *[]item.Stack)
}

var _ player.Handler = (*legacyBlockBreakHandler)(nil)
var _ player.BlockBreakDropsHandler = (*blockBreakDropsHandler)(nil)

func (h *blockBreakDropsHandler) HandleBlockBreakDrops(p *player.Player, pos cube.Pos, broken world.Block, drops *[]item.Stack) {
	h.dropsCalls++
	if h.dropsFn != nil {
		h.dropsFn(p, pos, broken, drops)
	}
}

func withBlockBreakDropsPlayer(t *testing.T, mode world.GameMode, run func(*player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	nativePosition := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: nativePosition}.New(player.Type, player.Config{
		Name: "Block Break Drops Test", Position: nativePosition, GameMode: mode, Experience: 11,
	})
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*player.Player)
		p.SetHeldItems(item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1), item.NewStack(item.Stick{}, 7))
		run(p)
	}).Wait(t.Context()); err != nil {
		t.Fatalf("world transaction: %v", err)
	}
}

func insertBreakDrops(p *player.Player, drops *[]item.Stack) {
	remaining := (*drops)[:0]
	for _, drop := range *drops {
		n, _ := p.Inventory().AddItem(drop)
		if rest := drop.Grow(-n); !rest.Empty() {
			remaining = append(remaining, rest)
		}
	}
	*drops = remaining
}

func breakGround(tx *world.Tx) (drops []item.Stack, xp int) {
	for e := range tx.Entities() {
		if e, ok := e.(*entity.Ent); ok {
			switch b := e.Behaviour().(type) {
			case *entity.ItemBehaviour:
				drops = append(drops, b.Item())
			case *entity.ExperienceOrbBehaviour:
				xp += b.Experience()
			}
		}
	}
	return
}

func stackCount(stacks []item.Stack, match item.Stack) (count int) {
	for _, stack := range stacks {
		if stack.Comparable(match) {
			count += stack.Count()
		}
	}
	return
}

func assertBreakAir(t *testing.T, p *player.Player, pos cube.Pos) {
	t.Helper()
	if _, ok := p.Tx().Block(pos).(block.Air); !ok {
		t.Fatalf("accepted block = %T, want air", p.Tx().Block(pos))
	}
}

func assertBreakState(t *testing.T, p *player.Player, tool, left item.Stack, exhaustion float64, experience int) {
	t.Helper()
	main, off := p.HeldItems()
	if !main.Equal(tool) || !off.Equal(left) {
		t.Fatalf("held items = %v/%v, want %v/%v", main, off, tool, left)
	}
	if data := p.Data(); math.Abs(data.Exhaustion-exhaustion) > 1e-12 || data.Experience != experience {
		t.Fatalf("exhaustion/experience = %v/%d, want %v/%d", data.Exhaustion, data.Experience, exhaustion, experience)
	}
}
