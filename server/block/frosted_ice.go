package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

const maxFrostedIceAge = 3

// FrostedIce is a temporary form of ice created when a player walks over source water with Frost Walker boots.
type FrostedIce struct {
	solid

	// Age controls how close the block is to melting. Values range from 0 to 3.
	Age int
}

// Instrument ...
func (FrostedIce) Instrument() sound.Instrument {
	return sound.Chimes()
}

// BreakInfo ...
func (f FrostedIce) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, pickaxeEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		return nil
	}).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if gm, ok := u.(interface{ GameMode() world.GameMode }); ok && gm.GameMode().CreativeInventory() {
			return
		}
		held, _ := u.HeldItems()
		if _, ok := held.Enchantment(enchantment.SilkTouch); ok {
			return
		}
		tx.SetBlock(pos, Water{Depth: 8, Still: true}, nil)
	})
}

// Friction ...
func (FrostedIce) Friction() float64 {
	return 0.98
}

// LightDiffusionLevel ...
func (FrostedIce) LightDiffusionLevel() uint8 {
	return 2
}

// NeighbourUpdateTick ...
func (f FrostedIce) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, f, frostedIceDelay(nil))
}

// RandomTick ...
func (f FrostedIce) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	f.decay(pos, tx, r)
}

// ScheduledTick ...
func (f FrostedIce) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	f.decay(pos, tx, r)
}

func (f FrostedIce) decay(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if f.hasAdjacentIce(pos, tx, 4) && r.IntN(3) != 0 {
		tx.ScheduleBlockUpdate(pos, f, frostedIceDelay(r))
		return
	}
	if f.highestAdjacentLight(pos, tx) < uint8(12-f.Age) {
		tx.ScheduleBlockUpdate(pos, f, frostedIceDelay(r))
		return
	}
	if !f.melt(pos, tx, r) {
		return
	}
	pos.Neighbours(func(neighbour cube.Pos) {
		b, loaded := tx.BlockLoaded(neighbour)
		if !loaded {
			return
		}
		if ice, ok := b.(FrostedIce); ok {
			ice.melt(neighbour, tx, r)
		}
	}, tx.Range())
}

func (f FrostedIce) melt(pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	if f.Age >= maxFrostedIceAge {
		tx.SetBlock(pos, Water{Depth: 8, Still: true}, nil)
		return true
	}
	f.Age++
	tx.SetBlock(pos, f, nil)
	tx.ScheduleBlockUpdate(pos, f, frostedIceDelay(r))
	return false
}

func (FrostedIce) hasAdjacentIce(pos cube.Pos, tx *world.Tx, required int) bool {
	found := 0
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if x == 0 && z == 0 {
				continue
			}
			b, loaded := tx.BlockLoaded(pos.Add(cube.Pos{x, 0, z}))
			if !loaded {
				continue
			}
			if _, ok := b.(FrostedIce); ok {
				found++
				if found >= required {
					return true
				}
			}
		}
	}
	return false
}

func (FrostedIce) highestAdjacentLight(pos cube.Pos, tx *world.Tx) uint8 {
	reduction := skyLightReduction(tx.World().Time())
	var highest uint8
	pos.Neighbours(func(neighbour cube.Pos) {
		sky, block := tx.LightLevels(neighbour)
		if sky > reduction {
			sky -= reduction
		} else {
			sky = 0
		}
		highest = max(highest, sky, block)
	}, tx.Range())
	return highest
}

func skyLightReduction(tick int) uint8 {
	progress := float64(tick%24000) / 24000
	sunProgress := progress - 0.25
	if progress < 0.25 {
		sunProgress = progress + 0.75
	}
	diff := ((1 - (math.Cos(sunProgress*math.Pi)+1)/2) - sunProgress) / 3
	angle := (sunProgress + diff) * 2 * math.Pi
	percentage := max(0.0, min(1.0, -(math.Cos(angle)*2-0.5)))
	return uint8(percentage * 11)
}

func frostedIceDelay(r *rand.Rand) time.Duration {
	if r == nil {
		return time.Duration(20+rand.IntN(21)) * time.Second / 20
	}
	return time.Duration(20+r.IntN(21)) * time.Second / 20
}

// EncodeBlock ...
func (f FrostedIce) EncodeBlock() (string, map[string]any) {
	return "minecraft:frosted_ice", map[string]any{"age": int32(f.Age)}
}

// allFrostedIce returns all valid Frosted Ice states.
func allFrostedIce() (blocks []world.Block) {
	for age := 0; age <= maxFrostedIceAge; age++ {
		blocks = append(blocks, FrostedIce{Age: age})
	}
	return
}
