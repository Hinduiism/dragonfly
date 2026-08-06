package item

import (
	"fmt"
	"math"
	"reflect"
)

// EnchantingTableRule configures how an enchantment may be selected by an
// enchanting table.
type EnchantingTableRule struct {
	Enchantment    EnchantmentType
	MaxLevel       int
	Weight         int
	ExperienceCost int
}

// EnchantingTablePolicy is an immutable set of rules used to generate
// enchanting-table offers. An enchantment omitted from the policy cannot be
// selected by the table.
type EnchantingTablePolicy struct {
	rules map[EnchantmentType]EnchantingTableRule
}

// NewEnchantingTablePolicy validates rules and returns an immutable policy. An
// empty policy disables all enchanting-table offers. ExperienceCost may be
// zero to preserve the table's normal generated requirement and slot-based
// payment.
func NewEnchantingTablePolicy(rules []EnchantingTableRule) (*EnchantingTablePolicy, error) {
	policy := &EnchantingTablePolicy{rules: make(map[EnchantmentType]EnchantingTableRule, len(rules))}
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
		if rule.ExperienceCost < 0 || rule.ExperienceCost > math.MaxUint8 {
			return nil, fmt.Errorf("enchanting table enchantment %q experience cost must be between 0 and 255, got %d", rule.Enchantment.Name(), rule.ExperienceCost)
		}
		if totalWeight > math.MaxInt-rule.Weight {
			return nil, fmt.Errorf("enchanting table rule weights overflow int")
		}
		totalWeight += rule.Weight
		policy.rules[rule.Enchantment] = rule
	}
	return policy, nil
}

// Rule returns the configured rule for an enchantment and reports whether it
// is present in the policy.
func (p *EnchantingTablePolicy) Rule(enchantment EnchantmentType) (EnchantingTableRule, bool) {
	if p == nil {
		return EnchantingTableRule{}, false
	}
	rule, ok := p.rules[enchantment]
	return rule, ok
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
