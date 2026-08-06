package item

import (
	"fmt"
	"math"
	"reflect"
)

// EnchantingTableLevelCost overrides the experience requirement and payment
// for one generated enchantment level.
type EnchantingTableLevelCost struct {
	Level          int
	ExperienceCost int
}

// EnchantingTableRule configures how an enchantment may be selected and priced
// by an enchanting table.
type EnchantingTableRule struct {
	Enchantment     EnchantmentType
	MaxLevel        int
	Weight          int
	ExperienceCosts []EnchantingTableLevelCost
}

// EnchantingTablePolicyRule is an immutable compiled enchanting-table rule.
type EnchantingTablePolicyRule struct {
	MaxLevel        int
	Weight          int
	experienceCosts []enchantingTableExperienceCost
}

type enchantingTableExperienceCost struct {
	value    int
	override bool
}

// EnchantingTablePolicy is an immutable set of rules used to generate
// enchanting-table offers. An enchantment omitted from the policy cannot be
// selected by the table.
type EnchantingTablePolicy struct {
	rules map[EnchantmentType]EnchantingTablePolicyRule
}

// NewEnchantingTablePolicy validates rules and returns an immutable policy. An
// empty policy disables all enchanting-table offers. Levels omitted from
// ExperienceCosts retain the table's normal generated requirement and
// slot-based payment. An explicit zero cost is a valid override.
func NewEnchantingTablePolicy(rules []EnchantingTableRule) (*EnchantingTablePolicy, error) {
	policy := &EnchantingTablePolicy{rules: make(map[EnchantmentType]EnchantingTablePolicyRule, len(rules))}
	var totalWeight int
	for index, rule := range rules {
		if nilEnchantmentType(rule.Enchantment) {
			return nil, fmt.Errorf("enchanting table rule %d has a nil enchantment", index)
		}
		id, registered := EnchantmentID(rule.Enchantment)
		if !registered {
			return nil, fmt.Errorf("enchanting table rule %d uses unregistered enchantment %q", index, rule.Enchantment.Name())
		}
		if id < 0 || id > math.MaxUint8 {
			return nil, fmt.Errorf("enchanting table enchantment %q has protocol ID %d outside 0..255", rule.Enchantment.Name(), id)
		}
		if _, exists := policy.rules[rule.Enchantment]; exists {
			return nil, fmt.Errorf("enchanting table enchantment %q is configured more than once", rule.Enchantment.Name())
		}
		if rule.MaxLevel < 1 || rule.MaxLevel > math.MaxUint8 {
			return nil, fmt.Errorf("enchanting table enchantment %q maximum level must be between 1 and 255, got %d", rule.Enchantment.Name(), rule.MaxLevel)
		}
		if rule.Weight <= 0 {
			return nil, fmt.Errorf("enchanting table enchantment %q weight must be positive, got %d", rule.Enchantment.Name(), rule.Weight)
		}
		experienceCosts := make([]enchantingTableExperienceCost, rule.MaxLevel+1)
		for costIndex, levelCost := range rule.ExperienceCosts {
			if levelCost.Level < 1 || levelCost.Level > rule.MaxLevel {
				return nil, fmt.Errorf("enchanting table enchantment %q experience cost %d has level %d outside 1..%d", rule.Enchantment.Name(), costIndex, levelCost.Level, rule.MaxLevel)
			}
			if levelCost.ExperienceCost < 0 || levelCost.ExperienceCost > math.MaxUint8 {
				return nil, fmt.Errorf("enchanting table enchantment %q level %d experience cost must be between 0 and 255, got %d", rule.Enchantment.Name(), levelCost.Level, levelCost.ExperienceCost)
			}
			if experienceCosts[levelCost.Level].override {
				return nil, fmt.Errorf("enchanting table enchantment %q level %d experience cost is configured more than once", rule.Enchantment.Name(), levelCost.Level)
			}
			experienceCosts[levelCost.Level] = enchantingTableExperienceCost{value: levelCost.ExperienceCost, override: true}
		}
		if totalWeight > math.MaxInt-rule.Weight {
			return nil, fmt.Errorf("enchanting table rule weights overflow int")
		}
		totalWeight += rule.Weight
		policy.rules[rule.Enchantment] = EnchantingTablePolicyRule{
			MaxLevel:        rule.MaxLevel,
			Weight:          rule.Weight,
			experienceCosts: experienceCosts,
		}
	}
	return policy, nil
}

// Rule returns the configured rule for an enchantment and reports whether it
// is present in the policy.
func (p *EnchantingTablePolicy) Rule(enchantment EnchantmentType) (EnchantingTablePolicyRule, bool) {
	if p == nil {
		return EnchantingTablePolicyRule{}, false
	}
	rule, ok := p.rules[enchantment]
	return rule, ok
}

// ExperienceCost returns the experience requirement and payment override for
// a generated enchantment level. If no override is configured, ok is false.
func (r EnchantingTablePolicyRule) ExperienceCost(level int) (cost int, ok bool) {
	if level < 1 || level >= len(r.experienceCosts) {
		return 0, false
	}
	configured := r.experienceCosts[level]
	return configured.value, configured.override
}

// Len returns the number of enchantments configured in the policy.
func (p *EnchantingTablePolicy) Len() int {
	if p == nil {
		return 0
	}
	return len(p.rules)
}

func nilEnchantmentType(enchantment EnchantmentType) bool {
	if enchantment == nil {
		return true
	}
	value := reflect.ValueOf(enchantment)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
