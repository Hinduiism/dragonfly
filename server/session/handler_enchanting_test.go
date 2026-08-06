package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

func TestAvailableEnchantingCandidatesUsesPolicy(t *testing.T) {
	policy := mustEnchantingPolicy(t, item.EnchantingTableRule{
		Enchantment: enchantment.Protection,
		MaxLevel:    1,
		Weight:      37,
	})
	stack := item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1)
	candidates := availableEnchantingCandidates(stack, 12, policy)
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	candidate := candidates[0]
	if candidate.enchantment.Type() != enchantment.Protection || candidate.enchantment.Level() != 1 {
		t.Fatalf("unexpected candidate: %s %d", candidate.enchantment.Type().Name(), candidate.enchantment.Level())
	}
	if candidate.weight != 37 {
		t.Fatalf("expected configured weight 37, got %d", candidate.weight)
	}
}

func TestAvailableEnchantingCandidatesAllowsExplicitTreasure(t *testing.T) {
	stack := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	for _, candidate := range availableEnchantingCandidates(stack, 30, nil) {
		if candidate.enchantment.Type() == enchantment.Mending {
			t.Fatal("default table candidates should exclude treasure enchantments")
		}
	}

	policy := mustEnchantingPolicy(t, item.EnchantingTableRule{
		Enchantment: enchantment.Mending,
		MaxLevel:    1,
		Weight:      2,
	})
	candidates := availableEnchantingCandidates(stack, 30, policy)
	if len(candidates) != 1 || candidates[0].enchantment.Type() != enchantment.Mending {
		t.Fatalf("expected explicit Mending candidate, got %+v", candidates)
	}
}

func TestAvailableEnchantingCandidatesSupportsUnbreakingFive(t *testing.T) {
	policy := mustEnchantingPolicy(t, item.EnchantingTableRule{
		Enchantment: enchantment.Unbreaking,
		MaxLevel:    5,
		Weight:      5,
	})
	stack := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	candidates := availableEnchantingCandidates(stack, 37, policy)
	if len(candidates) != 1 || candidates[0].enchantment.Level() != 5 {
		t.Fatalf("expected Unbreaking V candidate, got %+v", candidates)
	}
}

func TestNewEnchantingOfferPricing(t *testing.T) {
	primary := item.NewEnchantment(enchantment.Protection, 1)
	bonus := item.NewEnchantment(enchantment.Unbreaking, 1)

	defaultOffer := newEnchantingOffer(2, 30, []item.Enchantment{primary, bonus}, nil)
	if defaultOffer.requirement != 30 || defaultOffer.experienceCost != 3 || defaultOffer.lapisCost != 3 {
		t.Fatalf("unexpected default pricing: %+v", defaultOffer)
	}

	fallbackPolicy := mustEnchantingPolicy(t, item.EnchantingTableRule{
		Enchantment: enchantment.Protection,
		MaxLevel:    2,
		Weight:      10,
	})
	fallbackOffer := newEnchantingOffer(1, 17, []item.Enchantment{primary}, fallbackPolicy)
	if fallbackOffer.requirement != 17 || fallbackOffer.experienceCost != 2 || fallbackOffer.lapisCost != 2 {
		t.Fatalf("unexpected fallback pricing: %+v", fallbackOffer)
	}

	overridePolicy := mustEnchantingPolicy(t,
		item.EnchantingTableRule{Enchantment: enchantment.Protection, MaxLevel: 2, Weight: 10, ExperienceCost: 8},
		item.EnchantingTableRule{Enchantment: enchantment.Unbreaking, MaxLevel: 5, Weight: 5, ExperienceCost: 99},
	)
	overrideOffer := newEnchantingOffer(2, 30, []item.Enchantment{primary, bonus}, overridePolicy)
	if overrideOffer.requirement != 8 || overrideOffer.experienceCost != 8 || overrideOffer.lapisCost != 3 {
		t.Fatalf("unexpected override pricing: %+v", overrideOffer)
	}
}

func TestSelectEnchantingOffer(t *testing.T) {
	valid := enchantingOffer{slot: 1, enchantments: []item.Enchantment{item.NewEnchantment(enchantment.Protection, 1)}}
	if selected, err := selectEnchantingOffer([]enchantingOffer{valid}, 1); err != nil || selected.slot != 1 {
		t.Fatalf("select valid offer: %+v, %v", selected, err)
	}
	if _, err := selectEnchantingOffer([]enchantingOffer{{slot: 1}}, 1); err == nil {
		t.Fatal("expected empty offer to be rejected")
	}
	if _, err := selectEnchantingOffer([]enchantingOffer{valid}, 3); err == nil {
		t.Fatal("expected out-of-range recipe ID to be rejected")
	}
}

func TestWeightedEnchantmentBuckets(t *testing.T) {
	candidates := []enchantingCandidate{
		{enchantment: item.NewEnchantment(enchantment.Protection, 1), weight: 2},
		{enchantment: item.NewEnchantment(enchantment.Unbreaking, 1), weight: 3},
	}
	want := []item.EnchantmentType{
		enchantment.Protection,
		enchantment.Protection,
		enchantment.Unbreaking,
		enchantment.Unbreaking,
		enchantment.Unbreaking,
	}
	for bucket, expected := range want {
		if got := weightedEnchantmentAt(candidates, bucket).enchantment.Type(); got != expected {
			t.Fatalf("bucket %d selected %s, expected %s", bucket, got.Name(), expected.Name())
		}
	}
}

func mustEnchantingPolicy(t *testing.T, rules ...item.EnchantingTableRule) *item.EnchantingTablePolicy {
	t.Helper()
	policy, err := item.NewEnchantingTablePolicy(rules)
	if err != nil {
		t.Fatalf("new enchanting policy: %v", err)
	}
	return policy
}
