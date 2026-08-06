package item_test

import (
	"math"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestEnchantingTablePolicy(t *testing.T) {
	costs := []item.EnchantingTableLevelCost{
		{Level: 1, ExperienceCost: 7},
		{Level: 2, ExperienceCost: 12},
	}
	rules := []item.EnchantingTableRule{{
		Enchantment:     enchantment.Protection,
		MaxLevel:        2,
		Weight:          10,
		ExperienceCosts: costs,
	}}
	policy, err := item.NewEnchantingTablePolicy(rules)
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	rules[0].MaxLevel = 1
	costs[0].ExperienceCost = 99

	if policy.Len() != 1 {
		t.Fatalf("expected one rule, got %d", policy.Len())
	}
	rule, ok := policy.Rule(enchantment.Protection)
	if !ok {
		t.Fatal("expected protection rule")
	}
	if rule.MaxLevel != 2 || rule.Weight != 10 {
		t.Fatalf("unexpected copied rule: %+v", rule)
	}
	if cost, ok := rule.ExperienceCost(1); !ok || cost != 7 {
		t.Fatalf("unexpected level-one cost: %d, %t", cost, ok)
	}
	if cost, ok := rule.ExperienceCost(2); !ok || cost != 12 {
		t.Fatalf("unexpected level-two cost: %d, %t", cost, ok)
	}
	if _, ok := rule.ExperienceCost(0); ok {
		t.Fatal("unexpected out-of-range experience cost")
	}
	if _, ok := policy.Rule(enchantment.FireProtection); ok {
		t.Fatal("unexpected missing rule")
	}
}

func TestEnchantingTablePolicyExperienceCostOverride(t *testing.T) {
	policy, err := item.NewEnchantingTablePolicy([]item.EnchantingTableRule{
		{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1},
		{Enchantment: enchantment.FireProtection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 1, ExperienceCost: 7}}},
		{Enchantment: enchantment.FeatherFalling, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 1, ExperienceCost: 0}}},
	})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	defaultRule, _ := policy.Rule(enchantment.Protection)
	positiveRule, _ := policy.Rule(enchantment.FireProtection)
	zeroRule, _ := policy.Rule(enchantment.FeatherFalling)
	if _, ok := defaultRule.ExperienceCost(1); ok {
		t.Fatal("omitted experience cost should retain normal table pricing")
	}
	if cost, ok := positiveRule.ExperienceCost(1); !ok || cost != 7 {
		t.Fatalf("positive experience cost should enable its override: %d, %t", cost, ok)
	}
	if cost, ok := zeroRule.ExperienceCost(1); !ok || cost != 0 {
		t.Fatalf("explicit zero override should remain enabled: %d, %t", cost, ok)
	}
}

func TestEnchantingTablePolicyEmptyAndNil(t *testing.T) {
	policy, err := item.NewEnchantingTablePolicy(nil)
	if err != nil {
		t.Fatalf("new empty policy: %v", err)
	}
	if policy.Len() != 0 {
		t.Fatalf("expected empty policy, got %d rules", policy.Len())
	}
	var nilPolicy *item.EnchantingTablePolicy
	if nilPolicy.Len() != 0 {
		t.Fatal("nil policy should have zero rules")
	}
	if _, ok := nilPolicy.Rule(enchantment.Protection); ok {
		t.Fatal("nil policy should not contain rules")
	}
}

func TestEnchantingTablePolicyRejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name  string
		rules []item.EnchantingTableRule
		want  string
	}{
		{name: "nil", rules: []item.EnchantingTableRule{{}}, want: "nil enchantment"},
		{name: "typed nil", rules: []item.EnchantingTableRule{{Enchantment: (*testEnchantment)(nil), MaxLevel: 1, Weight: 1}}, want: "nil enchantment"},
		{name: "unregistered", rules: []item.EnchantingTableRule{{Enchantment: testEnchantment{name: "Test"}, MaxLevel: 1, Weight: 1}}, want: "unregistered enchantment"},
		{name: "duplicate", rules: []item.EnchantingTableRule{
			{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1},
			{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1},
		}, want: "configured more than once"},
		{name: "level zero", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 0, Weight: 1}}, want: "maximum level"},
		{name: "level high", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 256, Weight: 1}}, want: "maximum level"},
		{name: "weight zero", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 0}}, want: "weight must be positive"},
		{name: "cost level zero", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 0, ExperienceCost: 1}}}}, want: "outside 1..1"},
		{name: "cost level above maximum", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 2, ExperienceCost: 1}}}}, want: "outside 1..1"},
		{name: "cost duplicate level", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 1, ExperienceCost: 1}, {Level: 1, ExperienceCost: 2}}}}, want: "configured more than once"},
		{name: "cost negative", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 1, ExperienceCost: -1}}}}, want: "experience cost"},
		{name: "cost high", rules: []item.EnchantingTableRule{{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: 1, ExperienceCosts: []item.EnchantingTableLevelCost{{Level: 1, ExperienceCost: 256}}}}, want: "experience cost"},
		{name: "weight overflow", rules: []item.EnchantingTableRule{
			{Enchantment: enchantment.Protection, MaxLevel: 1, Weight: math.MaxInt},
			{Enchantment: enchantment.FireProtection, MaxLevel: 1, Weight: 1},
		}, want: "weights overflow"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := item.NewEnchantingTablePolicy(test.rules)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestEnchantingTablePolicyAllowsExplicitTreasure(t *testing.T) {
	policy, err := item.NewEnchantingTablePolicy([]item.EnchantingTableRule{{
		Enchantment: enchantment.Mending,
		MaxLevel:    1,
		Weight:      2,
	}})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	if _, ok := policy.Rule(enchantment.Mending); !ok {
		t.Fatal("expected explicit treasure enchantment")
	}
}

type testEnchantment struct {
	name string
}

func (t testEnchantment) Name() string                                      { return t.name }
func (testEnchantment) MaxLevel() int                                       { return 1 }
func (testEnchantment) Cost(int) (int, int)                                 { return 1, 1 }
func (testEnchantment) Rarity() item.EnchantmentRarity                      { return item.EnchantmentRarityCommon }
func (testEnchantment) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }
func (testEnchantment) CompatibleWithItem(world.Item) bool                  { return true }

func (*testEnchantment) pointerMarker() {}
