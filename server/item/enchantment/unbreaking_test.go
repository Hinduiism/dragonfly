package enchantment

import "testing"

func TestUnbreakingMaximumLevel(t *testing.T) {
	if Unbreaking.MaxLevel() != 5 {
		t.Fatalf("expected maximum level 5, got %v", Unbreaking.MaxLevel())
	}
	minimum, maximum := Unbreaking.Cost(5)
	if minimum != 37 || maximum != 87 {
		t.Fatalf("unexpected level 5 costs: %v-%v", minimum, maximum)
	}
}
