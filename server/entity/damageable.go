package entity

import "github.com/df-mc/dragonfly/server/world"

// behaviourDamageable represents a Behaviour that may be hurt directly without
// implementing Living.
type behaviourDamageable interface {
	Hurt(e *Ent, damage float64, src world.DamageSource) (n float64, vulnerable bool)
}

// HurtEntity hurts an entity if it is either Living or has a Behaviour that may
// be hurt directly. It returns the damage dealt, whether the entity was
// vulnerable to the damage, and whether the entity could be damaged.
func HurtEntity(e world.Entity, damage float64, src world.DamageSource) (n float64, vulnerable, ok bool) {
	result, ok := HurtEntityWithResult(e, damage, src)
	return result.Damage, result.Vulnerable && !result.KnockbackOnly, ok
}

// HitResult distinguishes an ordinary hit from motion-only acceptance. Legacy
// Hurt callers must not treat motion-only acceptance as permission for effects.
type HitResult struct {
	Damage        float64
	Vulnerable    bool
	KnockbackOnly bool
}

// HurtEntityWithResult is the result-aware variant of HurtEntity. Non-player
// Living and behaviour damage implementations keep their existing semantics.
func HurtEntityWithResult(e world.Entity, damage float64, src world.DamageSource) (HitResult, bool) {
	if target, ok := e.(interface {
		HurtWithResult(float64, world.DamageSource) HitResult
	}); ok {
		return target.HurtWithResult(damage, src), true
	}
	if l, ok := e.(Living); ok {
		n, vulnerable := l.Hurt(damage, src)
		return HitResult{Damage: n, Vulnerable: vulnerable}, true
	}
	if ent, ok := e.(*Ent); ok {
		if d, ok := ent.Behaviour().(behaviourDamageable); ok {
			n, vulnerable := d.Hurt(ent, damage, src)
			return HitResult{Damage: n, Vulnerable: vulnerable}, true
		}
	}
	return HitResult{}, false
}

// DamageableEntity checks if an entity may be damaged.
func DamageableEntity(e world.Entity) bool {
	if _, ok := e.(Living); ok {
		return true
	}
	if ent, ok := e.(*Ent); ok {
		_, ok = ent.Behaviour().(behaviourDamageable)
		return ok
	}
	return false
}
