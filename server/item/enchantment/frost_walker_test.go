package enchantment

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func TestFrostWalkerMetadata(t *testing.T) {
	if FrostWalker.Name() != "Frost Walker" {
		t.Fatalf("unexpected name %q", FrostWalker.Name())
	}
	if FrostWalker.MaxLevel() != 2 {
		t.Fatalf("expected maximum level 2, got %v", FrostWalker.MaxLevel())
	}
	if FrostWalker.Rarity() != item.EnchantmentRarityRare {
		t.Fatalf("expected rare rarity, got %v", FrostWalker.Rarity().Name())
	}
	if !FrostWalker.Treasure() {
		t.Fatal("expected Frost Walker to be a treasure enchantment")
	}
	for level := 1; level <= FrostWalker.MaxLevel(); level++ {
		minCost, maxCost := FrostWalker.Cost(level)
		if minCost != level*10 || maxCost != level*10+15 {
			t.Fatalf("unexpected level %v costs: %v-%v", level, minCost, maxCost)
		}
	}
}

func TestFrostWalkerCompatibility(t *testing.T) {
	if !FrostWalker.CompatibleWithItem(item.Boots{Tier: item.ArmourTierLeather{}}) {
		t.Fatal("expected boots to be compatible")
	}
	if FrostWalker.CompatibleWithItem(item.Sword{Tier: item.ToolTierWood}) {
		t.Fatal("expected a sword to be incompatible")
	}
	if !FrostWalker.CompatibleWithEnchantment(DepthStrider) {
		t.Fatal("expected Depth Strider to remain compatible")
	}
}

func TestFrostWalkerRegistration(t *testing.T) {
	e, ok := item.EnchantmentByID(25)
	if !ok || e != FrostWalker {
		t.Fatalf("expected Frost Walker at enchantment ID 25, got %v, %v", e, ok)
	}
	id, ok := item.EnchantmentID(FrostWalker)
	if !ok || id != 25 {
		t.Fatalf("expected enchantment ID 25, got %v, %v", id, ok)
	}
}

func TestUnbreakingMaximumLevel(t *testing.T) {
	if Unbreaking.MaxLevel() != 4 {
		t.Fatalf("expected maximum level 4, got %v", Unbreaking.MaxLevel())
	}
}
