package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

const (
	frostWalkerMovementThreshold = 0.00001
	maxFrostWalkerRadius         = 4
	frostWalkerGridSize          = maxFrostWalkerRadius*2 + 1
)

func (p *Player) applyFrostWalker(deltaPos [3]float64) {
	if math.Abs(deltaPos[0]) <= frostWalkerMovementThreshold && math.Abs(deltaPos[2]) <= frostWalkerMovementThreshold {
		return
	}
	boots := p.Armour().Boots()
	e, ok := boots.Enchantment(enchantment.FrostWalker)
	if !ok || e.Level() < 1 || e.Level() > enchantment.FrostWalker.MaxLevel() {
		return
	}

	radius := e.Level() + 2
	position := p.Position()
	baseX, targetY, baseZ := int(math.Floor(position[0])), int(math.Floor(position[1]))-1, int(math.Floor(position[2]))
	minX, minZ := baseX-radius, baseZ-radius

	var candidates [frostWalkerGridSize][frostWalkerGridSize]bool
	candidateCount := 0
	firstX, firstZ, lastX, lastZ := 0, 0, 0, 0
	for x := minX; x <= baseX+radius; x++ {
		for z := minZ; z <= baseZ+radius; z++ {
			pos := cube.Pos{x, targetY, z}
			b, loaded := p.tx.BlockLoaded(pos)
			water, ok := b.(block.Water)
			if !loaded || !ok || water.Depth != 8 || !water.Still || water.Falling {
				continue
			}
			above, loaded := p.tx.BlockLoaded(pos.Side(cube.FaceUp))
			if !loaded {
				continue
			}
			if _, air := above.(block.Air); !air {
				continue
			}

			gridX, gridZ := x-minX, z-minZ
			candidates[gridX][gridZ] = true
			if candidateCount == 0 {
				firstX, lastX, firstZ, lastZ = x, x, z, z
			} else {
				firstX, lastX = min(firstX, x), max(lastX, x)
				firstZ, lastZ = min(firstZ, z), max(lastZ, z)
			}
			candidateCount++
		}
	}
	if candidateCount == 0 {
		return
	}

	var occupied [frostWalkerGridSize][frostWalkerGridSize]bool
	search := cube.Box(float64(firstX), float64(targetY), float64(firstZ), float64(lastX+1), float64(targetY+1), float64(lastZ+1)).Grow(2)
	for e := range p.tx.EntitiesWithin(search) {
		box := e.H().Type().BBox(e).Translate(e.Position())
		fromX := max(firstX, int(math.Floor(box.Min()[0])))
		toX := min(lastX, int(math.Floor(box.Max()[0])))
		fromZ := max(firstZ, int(math.Floor(box.Min()[2])))
		toZ := min(lastZ, int(math.Floor(box.Max()[2])))
		for x := fromX; x <= toX; x++ {
			for z := fromZ; z <= toZ; z++ {
				gridX, gridZ := x-minX, z-minZ
				if !candidates[gridX][gridZ] || occupied[gridX][gridZ] {
					continue
				}
				cell := cube.Box(float64(x), float64(targetY), float64(z), float64(x+1), float64(targetY+1), float64(z+1))
				if box.IntersectsWith(cell) {
					occupied[gridX][gridZ] = true
				}
			}
		}
	}

	changes := make([]world.BlockChange, 0, candidateCount)
	for x := minX; x <= baseX+radius; x++ {
		for z := minZ; z <= baseZ+radius; z++ {
			gridX, gridZ := x-minX, z-minZ
			if candidates[gridX][gridZ] && !occupied[gridX][gridZ] {
				changes = append(changes, world.BlockChange{Position: cube.Pos{x, targetY, z}, Block: block.FrostedIce{}})
			}
		}
	}
	p.tx.SetBlocks(changes, nil)
}
