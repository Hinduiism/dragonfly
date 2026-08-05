package player

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFrostWalkerMovementThreshold(t *testing.T) {
	w := frostWalkerTestWorld()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, 1)
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, block.Water{Depth: 8, Still: true}, nil)

		p.applyFrostWalker(mgl64.Vec3{frostWalkerMovementThreshold, 0, 0})
		if _, ok := tx.Block(pos).(block.Water); !ok {
			t.Fatal("movement at the threshold froze water")
		}
		p.applyFrostWalker(mgl64.Vec3{frostWalkerMovementThreshold * 2, 0, 0})
		if _, ok := tx.Block(pos).(block.FrostedIce); !ok {
			t.Fatalf("movement above the threshold did not freeze water: %#v", tx.Block(pos))
		}
	})
}

func TestFrostWalkerScanExtents(t *testing.T) {
	tests := []struct {
		level   int
		edge    int
		outside int
	}{
		{level: 1, edge: 3, outside: 4},
		{level: 2, edge: 4, outside: 5},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("level_%d", test.level), func(t *testing.T) {
			w := frostWalkerTestWorld()
			defer w.Close()
			w.Do(func(tx *world.Tx) {
				p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, test.level)
				edge, outside := cube.Pos{test.edge, 64, 0}, cube.Pos{test.outside, 64, 0}
				tx.SetBlock(edge, block.Water{Depth: 8, Still: true}, nil)
				tx.SetBlock(outside, block.Water{Depth: 8, Still: true}, nil)
				p.applyFrostWalker(mgl64.Vec3{0.1, 0, 0})

				if _, ok := tx.Block(edge).(block.FrostedIce); !ok {
					t.Fatalf("expected edge position %v to freeze", edge)
				}
				if _, ok := tx.Block(outside).(block.Water); !ok {
					t.Fatalf("expected outside position %v to remain water", outside)
				}
			})
		})
	}
}

func TestFrostWalkerEligibleWater(t *testing.T) {
	w := frostWalkerTestWorld()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, 2)
		source := cube.Pos{0, 64, 0}
		flowing := cube.Pos{1, 64, 0}
		shallow := cube.Pos{2, 64, 0}
		blocked := cube.Pos{3, 64, 0}
		tx.SetBlock(source, block.Water{Depth: 8, Still: true}, nil)
		tx.SetBlock(flowing, block.Water{Depth: 8}, nil)
		tx.SetBlock(shallow, block.Water{Depth: 7, Still: true}, nil)
		tx.SetBlock(blocked, block.Water{Depth: 8, Still: true}, nil)
		tx.SetBlock(blocked.Side(cube.FaceUp), block.Stone{}, nil)

		p.applyFrostWalker(mgl64.Vec3{0.1, 0, 0})
		if _, ok := tx.Block(source).(block.FrostedIce); !ok {
			t.Fatal("still source water did not freeze")
		}
		for _, pos := range []cube.Pos{flowing, shallow, blocked} {
			if _, ok := tx.Block(pos).(block.Water); !ok {
				t.Fatalf("ineligible water at %v changed to %#v", pos, tx.Block(pos))
			}
		}
	})
}

func TestFrostWalkerSkipsOccupiedWater(t *testing.T) {
	w := frostWalkerTestWorld()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, 2)
		pos := cube.Pos{2, 64, 0}
		tx.SetBlock(pos, block.Water{Depth: 8, Still: true}, nil)
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 0}}.New(frostWalkerObstacleType{}, frostWalkerObstacleConfig{}))

		p.applyFrostWalker(mgl64.Vec3{0.1, 0, 0})
		if _, ok := tx.Block(pos).(block.Water); !ok {
			t.Fatalf("occupied water changed to %#v", tx.Block(pos))
		}
	})
}

func TestFrostWalkerMovementPaths(t *testing.T) {
	tests := []struct {
		name       string
		move       func(*Player)
		wantFrozen bool
	}{
		{name: "move", move: func(p *Player) { p.Move(mgl64.Vec3{0.1, 0, 0}, 0, 0) }, wantFrozen: true},
		{name: "displace", move: func(p *Player) { p.Displace(mgl64.Vec3{0.1, 0, 0}) }, wantFrozen: true},
		{name: "teleport", move: func(p *Player) { p.Teleport(mgl64.Vec3{0.6, 65, 0.5}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := frostWalkerTestWorld()
			defer w.Close()
			w.Do(func(tx *world.Tx) {
				p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, 1)
				pos := cube.Pos{0, 64, 0}
				tx.SetBlock(pos, block.Water{Depth: 8, Still: true}, nil)
				test.move(p)
				_, frozen := tx.Block(pos).(block.FrostedIce)
				if frozen != test.wantFrozen {
					t.Fatalf("frozen=%v, want %v; block=%#v", frozen, test.wantFrozen, tx.Block(pos))
				}
			})
		})
	}
}

func BenchmarkFrostWalkerDryLand(b *testing.B) {
	w := frostWalkerTestWorld()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := newFrostWalkerTestPlayer(tx, mgl64.Vec3{0.5, 65, 0.5}, 2)
		b.ResetTimer()
		for range b.N {
			p.applyFrostWalker(mgl64.Vec3{0.1, 0, 0})
		}
	})
}

func frostWalkerTestWorld() *world.World {
	return world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
}

func newFrostWalkerTestPlayer(tx *world.Tx, pos mgl64.Vec3, level int) *Player {
	handle := world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, Name: "Frost Walker Test"})
	p := tx.AddEntity(handle).(*Player)
	boots := item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1)
	if level > 0 {
		boots = boots.WithEnchantments(item.NewEnchantment(enchantment.FrostWalker, level))
	}
	p.Armour().SetBoots(boots)
	return p
}

type frostWalkerObstacleConfig struct{}

func (frostWalkerObstacleConfig) Apply(*world.EntityData) {}

type frostWalkerObstacleType struct{}

func (frostWalkerObstacleType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &frostWalkerObstacle{handle: handle, data: data}
}
func (frostWalkerObstacleType) EncodeEntity() string                        { return "dragonfly:frost_walker_test_obstacle" }
func (frostWalkerObstacleType) BBox(world.Entity) cube.BBox                 { return cube.Box(0, 0, 0, 1, 1, 1) }
func (frostWalkerObstacleType) DecodeNBT(map[string]any, *world.EntityData) {}
func (frostWalkerObstacleType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type frostWalkerObstacle struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (*frostWalkerObstacle) Close() error              { return nil }
func (e *frostWalkerObstacle) H() *world.EntityHandle  { return e.handle }
func (e *frostWalkerObstacle) Position() mgl64.Vec3    { return e.data.Pos }
func (e *frostWalkerObstacle) Rotation() cube.Rotation { return e.data.Rot }
