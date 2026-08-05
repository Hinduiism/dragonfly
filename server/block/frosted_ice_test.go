package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFrostedIceStates(t *testing.T) {
	for age := 0; age <= maxFrostedIceAge; age++ {
		b, ok := world.BlockByName("minecraft:frosted_ice", map[string]any{"age": int32(age)})
		ice, valid := b.(FrostedIce)
		if !ok || !valid || ice.Age != age {
			t.Fatalf("failed to resolve Frosted Ice age %v: %#v, %v", age, b, ok)
		}
		name, properties := ice.EncodeBlock()
		if name != "minecraft:frosted_ice" || properties["age"] != int32(age) {
			t.Fatalf("unexpected encoding for age %v: %q %#v", age, name, properties)
		}
	}
}

func TestFrostedIceAgesAndMelts(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	r := rand.New(rand.NewPCG(1, 2))
	pos := cube.Pos{8, 64, 8}

	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, FrostedIce{}, nil)
		for age := 1; age <= maxFrostedIceAge; age++ {
			ice := tx.Block(pos).(FrostedIce)
			ice.RandomTick(pos, tx, r)
			got := tx.Block(pos).(FrostedIce)
			if got.Age != age {
				t.Fatalf("expected age %v, got %v", age, got.Age)
			}
		}
		tx.Block(pos).(FrostedIce).RandomTick(pos, tx, r)
		water, ok := tx.Block(pos).(Water)
		if !ok || water.Depth != 8 || !water.Still || water.Falling {
			t.Fatalf("expected still source water after melting, got %#v", tx.Block(pos))
		}
	})
}

func TestFrostedIcePrimaryMeltAdvancesNeighbours(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	r := rand.New(rand.NewPCG(3, 4))
	pos, neighbour := cube.Pos{8, 64, 8}, cube.Pos{9, 64, 8}

	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, FrostedIce{Age: maxFrostedIceAge}, nil)
		tx.SetBlock(neighbour, FrostedIce{}, nil)
		tx.Block(pos).(FrostedIce).RandomTick(pos, tx, r)

		if _, ok := tx.Block(pos).(Water); !ok {
			t.Fatalf("expected primary block to melt, got %#v", tx.Block(pos))
		}
		ice, ok := tx.Block(neighbour).(FrostedIce)
		if !ok || ice.Age != 1 {
			t.Fatalf("expected direct neighbour at age 1, got %#v", tx.Block(neighbour))
		}
	})
}

func TestFrostedIceLightAndDensityRules(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	r := rand.New(rand.NewPCG(5, 6))

	w.SetTime(18000)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, FrostedIce{}, nil)
		tx.Block(pos).(FrostedIce).RandomTick(pos, tx, r)
		if ice := tx.Block(pos).(FrostedIce); ice.Age != 0 {
			t.Fatalf("expected low effective light to preserve age 0, got %v", ice.Age)
		}

		for i, offset := range []cube.Pos{{-1, 0, -1}, {-1, 0, 0}, {-1, 0, 1}, {0, 0, -1}} {
			tx.SetBlock(pos.Add(offset), FrostedIce{Age: i % 4}, nil)
		}
		if !(FrostedIce{}).hasAdjacentIce(pos, tx, 4) {
			t.Fatal("expected four horizontal Frosted Ice neighbours to satisfy density check")
		}
		tx.SetBlock(pos.Add(cube.Pos{0, 0, -1}), nil, nil)
		if (FrostedIce{}).hasAdjacentIce(pos, tx, 4) {
			t.Fatal("expected three horizontal Frosted Ice neighbours to fail density check")
		}
	})

	if got := skyLightReduction(0); got != 0 {
		t.Fatalf("expected no skylight reduction at noon, got %v", got)
	}
	if got := skyLightReduction(18000); got != 11 {
		t.Fatalf("expected skylight reduction 11 at midnight, got %v", got)
	}
}

func TestFrostedIceUsesBlockLightAtNight(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	column := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	lightPos := cube.Pos{9, 64, 8}
	column.SetBlock(uint8(lightPos.X()), int16(lightPos.Y()), uint8(lightPos.Z()), 0, world.DefaultBlockRegistry.BlockRuntimeID(Glowstone{}))
	provider := &frostedIceLightProvider{column: &chunk.Column{Chunk: column}}
	w := world.Config{Provider: provider, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	w.SetTime(18000)
	pos := cube.Pos{8, 64, 8}
	r := rand.New(rand.NewPCG(9, 10))

	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(lightPos).(Glowstone); !ok {
			t.Fatalf("expected loaded light source, got %#v", tx.Block(lightPos))
		}
		_, blockLight := tx.LightLevels(lightPos)
		if blockLight < 12 {
			t.Fatalf("expected strong block light, got %v", blockLight)
		}
		tx.SetBlock(pos, FrostedIce{}, nil)
		tx.Block(pos).(FrostedIce).RandomTick(pos, tx, r)
		if ice := tx.Block(pos).(FrostedIce); ice.Age != 1 {
			t.Fatalf("expected block light to advance ice at night, got age %v", ice.Age)
		}
	})
}

func TestFrostedIceSchedulesInitialDecay(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: rand.NewPCG(7, 8)}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	w.Do(func(tx *world.Tx) { tx.SetBlock(pos, FrostedIce{}, nil) })

	for range 42 {
		w.AdvanceTick()
	}
	w.Do(func(tx *world.Tx) {
		ice, ok := tx.Block(pos).(FrostedIce)
		if !ok || ice.Age == 0 {
			t.Fatalf("expected ordinary block updates to schedule decay, got %#v", tx.Block(pos))
		}
	})
}

func TestFrostedIceBreakResults(t *testing.T) {
	tests := []struct {
		name      string
		user      *frostedIceTestUser
		wantWater bool
	}{
		{name: "survival", user: &frostedIceTestUser{mode: world.GameModeSurvival}, wantWater: true},
		{name: "silk touch", user: &frostedIceTestUser{mode: world.GameModeSurvival, held: item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.SilkTouch, 1))}},
		{name: "creative", user: &frostedIceTestUser{mode: world.GameModeCreative}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			pos := cube.Pos{8, 64, 8}
			w.Do(func(tx *world.Tx) {
				tx.SetBlock(pos, nil, nil)
				(FrostedIce{}).BreakInfo().BreakHandler(pos, tx, test.user)
				_, water := tx.Block(pos).(Water)
				if water != test.wantWater {
					t.Fatalf("water=%v, want %v; block=%#v", water, test.wantWater, tx.Block(pos))
				}
			})
		})
	}
}

func TestSugarCaneSupportedByFrostedIce(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{8, 65, 8}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Dirt{}, nil)
		tx.SetBlock(pos.Add(cube.Pos{1, -1}), FrostedIce{}, nil)
		if !(SugarCane{}).canGrowHere(pos, tx, true) {
			t.Fatal("expected Frosted Ice to support adjacent sugar cane")
		}
	})
}

func TestFrostWalkerPreventsMagmaDamage(t *testing.T) {
	armour := inventory.NewArmour(nil)
	armour.SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.FrostWalker, 1)))
	protected := &magmaTestEntity{armour: armour}
	(Magma{}).EntityStepOn(cube.Pos{}, nil, protected)
	if protected.damage != 0 {
		t.Fatalf("expected no magma damage, got %v", protected.damage)
	}

	unprotected := &magmaTestEntity{armour: inventory.NewArmour(nil)}
	(Magma{}).EntityStepOn(cube.Pos{}, nil, unprotected)
	if unprotected.damage != 1 {
		t.Fatalf("expected one magma damage, got %v", unprotected.damage)
	}
}

type frostedIceTestUser struct {
	held item.Stack
	mode world.GameMode
}

func (*frostedIceTestUser) Close() error                          { return nil }
func (*frostedIceTestUser) H() *world.EntityHandle                { return nil }
func (*frostedIceTestUser) Position() mgl64.Vec3                  { return mgl64.Vec3{} }
func (*frostedIceTestUser) Rotation() cube.Rotation               { return cube.Rotation{} }
func (u *frostedIceTestUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *frostedIceTestUser) SetHeldItems(main, _ item.Stack)     { u.held = main }
func (*frostedIceTestUser) UsingItem() bool                       { return false }
func (*frostedIceTestUser) ReleaseItem()                          {}
func (*frostedIceTestUser) UseItem()                              {}
func (u *frostedIceTestUser) GameMode() world.GameMode            { return u.mode }

type magmaTestEntity struct {
	armour *inventory.Armour
	damage float64
}

type frostedIceLightProvider struct {
	world.NopProvider
	column *chunk.Column
}

func (p *frostedIceLightProvider) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	if pos == (world.ChunkPos{}) {
		return &chunk.Column{Chunk: p.column.Chunk.Clone()}, nil
	}
	return p.NopProvider.LoadColumn(pos, dim)
}

func (*magmaTestEntity) Close() error                { return nil }
func (*magmaTestEntity) H() *world.EntityHandle      { return nil }
func (*magmaTestEntity) Position() mgl64.Vec3        { return mgl64.Vec3{} }
func (*magmaTestEntity) Rotation() cube.Rotation     { return cube.Rotation{} }
func (e *magmaTestEntity) Armour() *inventory.Armour { return e.armour }
func (e *magmaTestEntity) Hurt(damage float64, _ world.DamageSource) (float64, bool) {
	e.damage += damage
	return damage, true
}
