package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestProjectileHitModesControlIgnitionAndPotionEffects(t *testing.T) {
	for _, mode := range []HitPolicy{HitNormal, HitKnockbackOnly, HitBlocked} {
		t.Run([]string{"normal", "motion", "blocked"}[mode], func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				h := &motionHitHandler{mode: mode}
				victim.Handle(h)
				opts := projectileKnockBackOpts()
				opts.Velocity = mgl64.Vec3{5, 0, 0}
				arrow := tx.AddEntity(entity.NewTippedArrow(opts, owner, potion.Poison())).(*entity.Ent)
				arrow.SetOnFire(time.Second * 5)
				arrow.Behaviour().(*entity.ProjectileBehaviour).Tick(arrow, tx)
				_, poisoned := victim.Effect(effect.Poison)
				if mode == HitNormal {
					if victim.Health() >= 20 || victim.OnFireDuration() == 0 || !poisoned || h.hurtCalls != 1 {
						t.Fatal("normal arrow behaviour regressed")
					}
					return
				}
				if victim.Health() != 20 || victim.OnFireDuration() != 0 || poisoned || h.hurtCalls != 0 {
					t.Fatal("protected arrow applied damage/effects")
				}
				if mode == HitKnockbackOnly && !victim.Velocity().ApproxEqualThreshold(mgl64.Vec3{0.45, 0.3608, 0}, 1e-12) {
					t.Fatalf("motion arrow changed physics: %v", victim.Velocity())
				}
				if mode == HitBlocked && victim.Velocity() != (mgl64.Vec3{}) {
					t.Fatal("blocked arrow applied motion")
				}
			})
		})
	}
}

func TestProtectedSplashAndLingeringEffects(t *testing.T) {
	for _, mode := range []HitPolicy{HitNormal, HitKnockbackOnly, HitBlocked} {
		t.Run([]string{"normal", "motion", "blocked"}[mode], func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				victim.Handle(&motionHitHandler{mode: mode})
				opts := projectileKnockBackOpts()
				opts.Velocity = mgl64.Vec3{5, 0, 0}
				splash := tx.AddEntity(entity.NewSplashPotion(opts, potion.Poison(), owner)).(*entity.Ent)
				splash.Behaviour().(*entity.ProjectileBehaviour).Tick(splash, tx)
				_, poisoned := victim.Effect(effect.Poison)
				if poisoned != (mode == HitNormal) {
					t.Fatal("splash ignored hit policy")
				}
				victim.RemoveEffect(effect.Poison)
				conf := entity.AreaEffectCloudBehaviourConfig{Potion: potion.Poison(), OwnerID: owner.H().UUID()}
				var data, decoded world.EntityData
				conf.Apply(&data)
				saved := entity.AreaEffectCloudType.EncodeNBT(&data)
				entity.AreaEffectCloudType.DecodeNBT(saved, &decoded)
				if entity.AreaEffectCloudType.EncodeNBT(&decoded)["df:owner_uuid"] != owner.H().UUID().String() {
					t.Fatal("cloud lost saved ownership")
				}
				cloud := tx.AddEntity(world.EntitySpawnOpts{Position: victim.Position().Sub(mgl64.Vec3{0, 0.25, 0})}.New(entity.AreaEffectCloudType, conf)).(*entity.Ent)
				for i := int64(0); i < 12; i++ {
					cloud.Tick(tx, i)
				}
				_, poisoned = victim.Effect(effect.Poison)
				if poisoned != (mode == HitNormal) {
					t.Fatal("lingering cloud ignored hit policy")
				}
				victim.RemoveEffect(effect.Poison)
				victim.SetHealth(10)
				heal := tx.AddEntity(world.EntitySpawnOpts{Position: victim.Position().Sub(mgl64.Vec3{0, 0.25, 0})}.New(entity.AreaEffectCloudType, entity.AreaEffectCloudBehaviourConfig{Potion: potion.Healing(), OwnerID: owner.H().UUID()})).(*entity.Ent)
				for i := int64(0); i < 12; i++ {
					heal.Tick(tx, i)
				}
				if victim.Health() <= 10 {
					t.Fatal("friendly protection blocked beneficial potion")
				}
			})
		})
	}
}

func TestProjectileOwnershipRoundTripAndOrphanedCloud(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
		for _, typ := range []world.EntityType{entity.ArrowType, entity.SnowballType, entity.EggType, entity.SplashPotionType, entity.LingeringPotionType, entity.EnderPearlType, entity.BottleOfEnchantingType} {
			var data, decoded world.EntityData
			entity.ProjectileBehaviourConfig{Owner: owner.H()}.Apply(&data)
			saved := typ.EncodeNBT(&data)
			typ.DecodeNBT(saved, &decoded)
			if typ.EncodeNBT(&decoded)["df:owner_uuid"] != owner.H().UUID().String() {
				t.Fatalf("%s lost saved ownership", typ.EncodeEntity())
			}
		}
		victim.Handle(&motionHitHandler{mode: HitNormal, orphanBlocked: true})
		cloud := tx.AddEntity(world.EntitySpawnOpts{Position: victim.Position().Sub(mgl64.Vec3{0, 0.25, 0})}.New(entity.AreaEffectCloudType, entity.AreaEffectCloudBehaviourConfig{Potion: potion.Poison(), OwnerID: owner.H().UUID()})).(*entity.Ent)
		tx.RemoveEntity(owner)
		for i := int64(0); i < 12; i++ {
			cloud.Tick(tx, i)
		}
		if _, poisoned := victim.Effect(effect.Poison); poisoned {
			t.Fatal("cloud became unowned and bypassed protection")
		}
	})
}

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
