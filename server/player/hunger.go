package player

import (
	"sync"
)

// hungerManager handles the changes in hunger, exhaustion and saturation of a player.
type hungerManager struct {
	mu              sync.RWMutex
	foodLevel       int
	saturationLevel float64
	exhaustionLevel float64
	foodTick        int
}

// newHungerManager returns a new hunger manager with the default values for food level, saturation level and
// exhaustion level.
func newHungerManager() *hungerManager {
	return &hungerManager{foodLevel: 20, saturationLevel: 20}
}

// Food returns the current food level of a player. The level returned is guaranteed to always be between 0
// and 20.
func (m *hungerManager) Food() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.foodLevel
}

// SetFood sets the food level of a player. The level passed must be in a range of 0-20. If the level passed
// is negative, the food level will be set to 0. If the level exceeds 20, the food level will be set to 20.
func (m *hungerManager) SetFood(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.setFood(level)
}

// setFood updates food and the healing cadence while mu is held. PM resets
// the timer only when food crosses a regeneration, sprinting or starvation band.
func (m *hungerManager) setFood(level int) {
	level = max(min(level, 20), 0)
	for _, bound := range [...]int{17, 6, 0} {
		if (m.foodLevel > bound) != (level > bound) {
			m.foodTick = 0
			break
		}
	}
	m.foodLevel = level
}

// AddFood adds a number of food points to the current food level of a player.
func (m *hungerManager) AddFood(points int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.setFood(m.foodLevel + points)
}

// Reset resets the hunger manager to its default values, identical to those set when creating a new manager
// using newHungerManager.
func (m *hungerManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = 20
	m.saturationLevel = 20
	m.exhaustionLevel = 0
	m.foodTick = 0
}

// resetExhaustion clears cancelled food loss without restarting natural healing.
// foodTick is the cadence before the attempted loss, including any band changes
// that exhaustion provisionally caused before the handler cancelled it.
func (m *hungerManager) resetExhaustion(foodTick int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exhaustionLevel = 0
	m.saturationLevel = 0
	m.foodTick = foodTick
}

// exhaust exhausts the player by the amount of points passed. If the total exhaustion level exceeds 4, a
// saturation point, or food point, if saturation is 0, will be subtracted.
func (m *hungerManager) exhaust(points float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.exhaustionLevel += points
	for m.exhaustionLevel >= 4 {
		// Maximum exhaustion value is 4, so keep removing one saturation point until the exhaustion level
		// is below 4.
		m.exhaustionLevel -= 4
		m.desaturate()
	}
}

// saturate saturates the player's food and saturation by the amount of points passed. Note that the total
// saturation will never exceed 20, independently of the current food value.
func (m *hungerManager) saturate(food int, saturation float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.setFood(m.foodLevel + food)
	m.saturationLevel = max(min(m.saturationLevel+saturation, 20), 0)
}

// desaturate removes one saturation point from the player. If the saturation level of the player is already
// 0, a point will be subtracted from the food level instead. If that level, too, is already 0, nothing will
// happen.
func (m *hungerManager) desaturate() {
	if m.saturationLevel <= 0 && m.foodLevel != 0 {
		m.setFood(m.foodLevel - 1)
	} else if m.saturationLevel > 0 {
		m.saturationLevel = max(m.saturationLevel-1, 0)
	}
}

// canRegenerate checks if the player with the amount of food levels in the hunger manager can regenerate.
// The function returns true if Food() returns either 18-20.
// The rate of regeneration is 1/4 seconds.
func (m *hungerManager) canRegenerate() bool {
	return m.Food() >= 18
}

// canSprint returns true if the food level of the player is 7 or higher.
func (m *hungerManager) canSprint() bool {
	return m.Food() > 6
}

// starving checks if the player is currently considered to be starving. True is returned if Food() returns 0.
func (m *hungerManager) starving() bool {
	return m.Food() == 0
}

// StarvationDamageSource is the world.DamageSource passed when a player is
// dealt damage from an empty food bar.
type StarvationDamageSource struct{}

func (StarvationDamageSource) ReducedByArmour() bool     { return false }
func (StarvationDamageSource) ReducedByResistance() bool { return false }
func (StarvationDamageSource) Fire() bool                { return false }
func (StarvationDamageSource) IgnoreTotem() bool         { return false }
