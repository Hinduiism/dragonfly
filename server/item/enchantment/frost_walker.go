package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// FrostWalker is a boot enchantment that freezes source water beneath a moving wearer.
var FrostWalker frostWalker

type frostWalker struct{}

// Name ...
func (frostWalker) Name() string {
	return "Frost Walker"
}

// MaxLevel ...
func (frostWalker) MaxLevel() int {
	return 2
}

// Cost ...
func (frostWalker) Cost(level int) (int, int) {
	minCost := level * 10
	return minCost, minCost + 15
}

// Rarity ...
func (frostWalker) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// Treasure ...
func (frostWalker) Treasure() bool {
	return true
}

// CompatibleWithEnchantment ...
func (frostWalker) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (frostWalker) CompatibleWithItem(i world.Item) bool {
	b, ok := i.(item.BootsType)
	return ok && b.Boots()
}
