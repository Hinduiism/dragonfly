package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type criticalTestHandler struct {
	NopHandler
	critical bool
	factor   float64
}

func (h criticalTestHandler) HandleAttackEntity(_ *Context, _ world.Entity, _, _ *float64, critical *bool) {
	*critical = h.critical
}

func (h criticalTestHandler) CriticalHitMultiplier() float64 { return h.factor }

// Use a living target that records the native mob attack's damage argument.
type criticalTestMob struct {
	*Player
	damage float64
}

func (m *criticalTestMob) Hurt(damage float64, _ world.DamageSource) (float64, bool) {
	m.damage = damage
	return damage, false
}

func TestCriticalHitMultiplierAppliesToPlayerAndMobMelee(t *testing.T) {
	for _, tc := range []struct {
		name     string
		critical bool
		factor   float64
		damage   float64
	}{
		{"normal hit", false, 1.3, 13},
		{"configured crit", true, 1.3, 16.9},
		{"default crit", true, 0, 19.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(_ *world.Tx, attacker, victim *Player) {
				attacker.Teleport(mgl64.Vec3{1, 64, -2})
				attacker.Handle(criticalTestHandler{critical: tc.critical, factor: tc.factor})
				sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).
					WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 4))
				attacker.SetHeldItems(sword, item.Stack{})
				mob := &criticalTestMob{Player: victim}
				if !attacker.AttackEntity(mob) || math.Abs(mob.damage-tc.damage) > 1e-9 {
					t.Fatalf("mob damage = %g, want %g", mob.damage, tc.damage)
				}
				before := victim.Health()
				if !attacker.AttackEntity(victim) || math.Abs(before-victim.Health()-tc.damage) > 1e-9 {
					t.Fatalf("player damage = %g, want %g", before-victim.Health(), tc.damage)
				}
			})
		})
	}
}
