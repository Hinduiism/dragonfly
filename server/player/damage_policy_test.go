package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

type motionHitHandler struct {
	NopHandler
	mode          HitPolicy
	orphanBlocked bool
	hurtCalls     int
	policyCalls   int
}

func (h *motionHitHandler) HandleHit(_ *Context, src world.DamageSource) HitPolicy {
	h.policyCalls++
	if s, ok := src.(entity.ProjectileDamageSource); ok && h.orphanBlocked && s.Owner == nil && s.OwnerID != [16]byte{} {
		return HitBlocked
	}
	return h.mode
}
func (h *motionHitHandler) HandleHurt(_ *Context, _ *float64, _ bool, _ *time.Duration, _ world.DamageSource) {
	h.hurtCalls++
}

func TestMotionOnlyHitDoesNotConsumeDamageImmunity(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(_ *world.Tx, owner, victim *Player) {
		h := &motionHitHandler{mode: HitKnockbackOnly}
		victim.Handle(h)
		victim.SetAbsorption(5)
		result := victim.HurtWithResult(7, entity.ProjectileDamageSource{Owner: owner})
		if !result.KnockbackOnly || !result.Vulnerable || result.Damage != 0 || victim.Health() != 20 || victim.Absorption() != 5 || h.hurtCalls != 0 {
			t.Fatalf("motion hit entered damage processing: result=%+v health=%v absorption=%v hooks=%d", result, victim.Health(), victim.Absorption(), h.hurtCalls)
		}
		if !victim.immuneUntil.IsZero() || !victim.projectileImmuneUntil.IsZero() || victim.directMelee.attackTime != 0 {
			t.Fatal("motion hit modified ordinary hit immunity")
		}
		if result := victim.HurtWithResult(7, entity.ProjectileDamageSource{Owner: owner}); result.Vulnerable {
			t.Fatal("repeated motion projectile bypassed its own timing")
		}
		victim.Handle(NopHandler{})
		if _, ok := victim.Hurt(7, entity.ProjectileDamageSource{Owner: owner}); !ok || victim.Health() != 18 || victim.Absorption() != 0 {
			t.Fatal("friendly motion prevented subsequent real projectile damage")
		}
		if r := victim.hurtByPlayerMelee(7, 7, owner, 0.4, 0.4); !r.accepted || victim.Health() != 11 {
			t.Fatal("friendly/projectile motion prevented subsequent enemy melee")
		}
	})
}

func TestLegacyHurtDoesNotAdvertiseMotionAsDamage(t *testing.T) {
	withDamageTestPlayer(t, func(p *Player) {
		p.Handle(&motionHitHandler{mode: HitKnockbackOnly})
		if n, accepted := p.Hurt(5, entity.ProjectileDamageSource{}); n != 0 || accepted || p.Health() != 20 {
			t.Fatal("legacy Hurt exposed a damage success for motion-only hit")
		}
	})
}

type ordinaryDamageSource struct{}

func (ordinaryDamageSource) ReducedByArmour() bool     { return false }
func (ordinaryDamageSource) ReducedByResistance() bool { return false }
func (ordinaryDamageSource) Fire() bool                { return false }
func (ordinaryDamageSource) IgnoreTotem() bool         { return true }

type absoluteDamageSource struct{ ordinaryDamageSource }

func (absoluteDamageSource) IgnoreAbsorption() bool     { return true }
func (absoluteDamageSource) IgnoreAttackImmunity() bool { return true }

type disabledCapabilities struct{ ordinaryDamageSource }

func (disabledCapabilities) IgnoreAbsorption() bool     { return false }
func (disabledCapabilities) IgnoreAttackImmunity() bool { return false }

func TestOrdinaryDamageStillConsumesAbsorptionAndUsesImmunity(t *testing.T) {
	withDamageTestPlayer(t, func(p *Player) {
		p.SetAbsorption(5)
		dealt, vulnerable := p.Hurt(2, ordinaryDamageSource{})
		if !vulnerable || dealt != 2 || p.Health() != 20 || p.Absorption() != 3 {
			t.Fatalf("first hurt = dealt %v vulnerable %t health %v absorption %v", dealt, vulnerable, p.Health(), p.Absorption())
		}
		if dealt, vulnerable = p.Hurt(2, ordinaryDamageSource{}); vulnerable || dealt != 0 {
			t.Fatalf("immune repeated hurt = dealt %v vulnerable %t, want rejected", dealt, vulnerable)
		}
	})
}

func TestAbsoluteDamageBypassesAbsorptionAndExistingImmunity(t *testing.T) {
	withDamageTestPlayer(t, func(p *Player) {
		p.SetAbsorption(5)
		p.immuneUntil = time.Now().Add(time.Minute)
		p.lastDamage = 100
		originalImmunity, originalLastDamage := p.immuneUntil, p.lastDamage

		dealt, vulnerable := p.Hurt(2, absoluteDamageSource{})
		if !vulnerable || dealt != 2 || p.Health() != 18 || p.Absorption() != 5 {
			t.Fatalf("absolute hurt = dealt %v vulnerable %t health %v absorption %v", dealt, vulnerable, p.Health(), p.Absorption())
		}
		if p.immuneUntil != originalImmunity || p.lastDamage != originalLastDamage {
			t.Fatalf("absolute hurt changed ordinary immunity: until=%s damage=%v", p.immuneUntil, p.lastDamage)
		}

		dealt, vulnerable = p.Hurt(2, absoluteDamageSource{})
		if !vulnerable || dealt != 2 || p.Health() != 16 || p.Absorption() != 5 {
			t.Fatalf("second absolute hurt = dealt %v vulnerable %t health %v absorption %v", dealt, vulnerable, p.Health(), p.Absorption())
		}
	})
}

func TestDisabledOptionalCapabilitiesRetainOrdinaryBehaviour(t *testing.T) {
	withDamageTestPlayer(t, func(p *Player) {
		p.SetAbsorption(2)
		dealt, vulnerable := p.Hurt(2, disabledCapabilities{})
		if !vulnerable || dealt != 2 || p.Health() != 20 || p.Absorption() != 0 {
			t.Fatalf("disabled capabilities changed ordinary damage behavior")
		}
	})
}

func withDamageTestPlayer(t *testing.T, test func(*Player)) {
	t.Helper()
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })
	handle := world.EntitySpawnOpts{}.New(Type, Config{Health: 20, MaxHealth: 20, GameMode: world.GameModeSurvival})
	if err := runtime.Do(func(tx *world.Tx) {
		added := tx.AddEntity(handle)
		p, ok := added.(*Player)
		if !ok {
			t.Fatalf("added entity = %T, want *Player", added)
		}
		test(p)
	}).Wait(t.Context()); err != nil {
		t.Fatalf("world transaction error = %v", err)
	}
}
