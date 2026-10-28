package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSnowballAndMeleeHaveIndependentCooldowns(t *testing.T) {
	for _, snowballFirst := range []bool{true, false} {
		name := "melee then snowball"
		if snowballFirst {
			name = "snowball then melee"
		}
		t.Run(name, func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				melee := func() {
					if result := victim.hurtByPlayerMelee(7, 7, owner, 0.4, 0.4); !result.accepted {
						t.Errorf("first melee hit rejected after snowball: %+v", result)
					}
				}
				snowball := func() {
					projectile := tx.AddEntity(entity.NewSnowball(projectileKnockBackOpts(), owner)).(*entity.Ent)
					projectile.Behaviour().(*entity.ProjectileBehaviour).Tick(projectile, tx)
				}
				if snowballFirst {
					snowball()
					melee()
				} else {
					melee()
					snowball()
				}
				// Both accepted impacts blend the previous motion, using the same
				// knockback calculation as two otherwise eligible melee hits.
				wantMotion := mgl64.Vec3{0.6, 0.4, 0}
				if victim.Health() != 13 || !victim.Velocity().ApproxEqualThreshold(wantMotion, 1e-12) {
					t.Errorf("combined hits: health=%v motion=%v, want 13 and %v", victim.Health(), victim.Velocity(), wantMotion)
				}
				if result := victim.hurtByPlayerMelee(7, 7, owner, 0.4, 0.4); result.accepted {
					t.Error("projectile reset the existing melee cooldown")
				}
				if _, vulnerable := victim.Hurt(0, entity.ProjectileDamageSource{Owner: owner}); vulnerable {
					t.Error("melee reset the existing projectile cooldown")
				}
			})
		})
	}
}

func TestArrowAndMeleeHaveIndependentCooldowns(t *testing.T) {
	for _, zeroDamage := range []bool{false, true} {
		name := "damaging arrow"
		if zeroDamage {
			name = "damage disabled by handler"
		}
		t.Run(name, func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				if zeroDamage {
					victim.Handle(zeroProjectileDamage{})
				}
				opts := projectileKnockBackOpts()
				opts.Velocity = mgl64.Vec3{5, 0, 0}
				projectile := tx.AddEntity(entity.NewArrow(opts, owner)).(*entity.Ent)
				projectile.Behaviour().(*entity.ProjectileBehaviour).Tick(projectile, tx)
				before := victim.Health()
				if zeroDamage && before != 20 || !zeroDamage && before >= 20 {
					t.Errorf("unexpected arrow health: %v", before)
				}
				victim.SetVelocity(mgl64.Vec3{})
				result := victim.hurtByPlayerMelee(7, 7, owner, 0.4, 0.4)
				wantMotion := mgl64.Vec3{0.4, 0.4, 0}
				if !result.accepted || victim.Health() != before-7 || !victim.Velocity().ApproxEqualThreshold(wantMotion, 1e-12) {
					t.Errorf("melee after arrow: accepted=%t health=%v motion=%v, want true, %v and %v", result.accepted, victim.Health(), victim.Velocity(), before-7, wantMotion)
				}
			})
		})
	}
}

func TestProjectileDamageCooldownKeepsStrongerHitDifference(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(_ *world.Tx, owner, victim *Player) {
		src := entity.ProjectileDamageSource{Owner: owner}
		if _, vulnerable := victim.Hurt(3, src); !vulnerable || victim.Health() != 17 {
			t.Error("initial projectile damage was not applied")
		}
		if _, vulnerable := victim.Hurt(3, src); vulnerable || victim.Health() != 17 {
			t.Error("equal projectile damage bypassed projectile immunity")
		}
		if result := victim.hurtByPlayerMelee(7, 7, owner, 0.4, 0.4); !result.accepted || victim.Health() != 10 {
			t.Errorf("projectile reduced or rejected subsequent melee damage: health=%v result=%+v", victim.Health(), result)
		}
		if _, vulnerable := victim.Hurt(5, src); !vulnerable || victim.Health() != 8 {
			t.Errorf("stronger projectile did not apply its own damage difference: health=%v vulnerable=%t", victim.Health(), vulnerable)
		}
	})
}

// Mirrors Survival's projectile policy, which cancels health damage while
// retaining projectile knockback. Its original damage must not gate melee.
type zeroProjectileDamage struct{ NopHandler }

func (zeroProjectileDamage) HandleHurt(_ *Context, damage *float64, _ bool, _ *time.Duration, source world.DamageSource) {
	if _, ok := source.(entity.ProjectileDamageSource); ok {
		*damage = 0
	}
}
