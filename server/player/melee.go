package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	directMeleeForce                = 0.4
	directMeleeVerticalLimit        = 0.4
	directMeleeCooldownTicks        = 10
	directMeleeSpawnProtectionTicks = 60
	directMeleeReach                = 8.0
	meleeWaterEyeOffset             = 0.1111111
)

type directMeleeDecision uint8

const (
	directMeleeProtected directMeleeDecision = iota
	directMeleeCold
	directMeleeWarmRejected
	directMeleeWarmStronger
)

// directMeleeState holds the transient state used to decide whether a direct melee hit produces motion. It is
// deliberately separate from Dragonfly's wall-clock health immunity.
type directMeleeState struct {
	attackTime      int64
	noDamageTicks   int64
	lastBaseDamage  float64
	motion          mgl64.Vec3
	motionSetTick   int64
	lastTick        int64
	tickInitialised bool
}

func newDirectMeleeState(spawnProtected bool) directMeleeState {
	state := directMeleeState{}
	if spawnProtected {
		state.noDamageTicks = directMeleeSpawnProtectionTicks
	}
	return state
}

func (state *directMeleeState) tick(current int64) {
	if !state.tickInitialised {
		state.lastTick, state.tickInitialised = current-1, true
	}
	delta := current - state.lastTick
	if delta <= 0 {
		return
	}
	state.lastTick = current
	state.attackTime = max(0, state.attackTime-delta)
	state.noDamageTicks = max(0, state.noDamageTicks-delta)
	if state.motionSetTick < current {
		state.motion = mgl64.Vec3{}
	}
}

func (state *directMeleeState) classify(baseDamage float64) directMeleeDecision {
	if state.noDamageTicks > 0 {
		return directMeleeProtected
	}
	if state.attackTime <= 0 {
		return directMeleeCold
	}
	if state.lastBaseDamage >= baseDamage {
		return directMeleeWarmRejected
	}
	return directMeleeWarmStronger
}

func (state *directMeleeState) commit(decision directMeleeDecision, baseDamage float64) {
	switch decision {
	case directMeleeCold:
		state.attackTime = directMeleeCooldownTicks
		state.lastBaseDamage = baseDamage
	case directMeleeWarmStronger:
		state.lastBaseDamage = baseDamage
	}
}

func (state *directMeleeState) recordMotion(motion mgl64.Vec3, current int64) {
	state.motion, state.motionSetTick = motion, current
}

func (state *directMeleeState) recordJump(vertical float64, current int64) {
	state.motion[1], state.motionSetTick = vertical, current
}

func (state *directMeleeState) clearMotion(current int64) {
	state.motion, state.motionSetTick = mgl64.Vec3{}, current
}

func (state *directMeleeState) resumeAt(current int64) {
	state.lastTick, state.tickInitialised = current, true
}

// meleeKnockBackMotion calculates direct-melee motion. Keep the reciprocal and multiplication operations in this
// order so protocol float conversion starts from the expected value.
func meleeKnockBackMotion(victim, attacker, previous mgl64.Vec3, force, verticalLimit float64) (mgl64.Vec3, bool) {
	x, z := victim[0]-attacker[0], victim[2]-attacker[2]
	f := math.Sqrt(x*x + z*z)
	if f <= 0 {
		return mgl64.Vec3{}, false
	}
	f = 1 / f

	motion := previous.Mul(0.5)
	motion[0] += x * f * force
	motion[1] += force
	motion[2] += z * f * force
	if motion[1] > verticalLimit {
		motion[1] = verticalLimit
	}
	return motion, true
}

func (p *Player) canMeleeInteract(target *Player) bool {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return false
	}
	eye, targetPos := entity.EyePosition(p), target.Position()
	if eye.Sub(targetPos).LenSqr() > directMeleeReach*directMeleeReach {
		return false
	}
	direction := p.Rotation().Vec3()
	return direction.Dot(targetPos)-direction.Dot(eye) >= -math.Sqrt(3)/2
}

func (p *Player) meleeCritical() bool {
	_, blind := p.Effect(effect.Blindness)
	return !p.Sprinting() && !p.Flying() && p.FallDistance() > 0 && !blind && !p.underwaterForMelee()
}

func (p *Player) underwaterForMelee() bool {
	eye := entity.EyePosition(p)
	pos := cube.PosFromVec3(eye)
	liquid, ok := p.tx.Liquid(pos)
	if !ok {
		return false
	}
	if _, ok := liquid.(block.Water); !ok {
		return false
	}
	return eye[1] < waterSurfaceHeight(float64(pos[1]), liquid.LiquidDepth(), liquid.LiquidFalling())
}

// waterSurfaceHeight converts Dragonfly's depth representation into the liquid surface used for eye-submersion
// checks. A source or falling block has decay 0; progressively shallower water has decay 1-7.
func waterSurfaceHeight(blockY float64, depth int, falling bool) float64 {
	decay := 8 - depth
	if falling {
		decay = 0
	}
	fluidHeightPercent := float64(decay+1) / 9
	return blockY + 1 - (fluidHeightPercent - meleeWaterEyeOffset)
}

func (p *Player) meleeSoundPosition(target *Player) mgl64.Vec3 {
	return target.Position().Add(mgl64.Vec3{0, Type.BBox(target).Height() / 2})
}

// attackPlayer handles the scoped direct player-versus-player path. Other AttackEntity targets remain on
// Dragonfly's normal combat implementation.
func (p *Player) attackPlayer(target *Player) bool {
	if target == p || target.Dead() {
		return false
	}
	soundPos := p.meleeSoundPosition(target)
	if !p.canMeleeInteract(target) {
		p.SwingArm()
		p.tx.PlaySound(soundPos, sound.Attack{})
		return false
	}

	force, verticalLimit := directMeleeForce, directMeleeVerticalLimit
	critical := p.meleeCritical()
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleAttackEntity(ctx, target, &force, &verticalLimit, &critical); ctx.Cancelled() {
		p.SwingArm()
		p.tx.PlaySound(soundPos, sound.Attack{})
		return false
	}

	held, _ := p.HeldItems()
	baseDamage := held.AttackDamage()
	damage := baseDamage
	if strength, ok := p.Effect(effect.Strength); ok {
		damage += damage * effect.Strength.Multiplier(strength.Level())
	}
	if weakness, ok := p.Effect(effect.Weakness); ok {
		damage -= damage * effect.Weakness.Multiplier(weakness.Level())
	}
	if sharpness, ok := held.Enchantment(enchantment.Sharpness); ok {
		damage += enchantment.Sharpness.Addend(sharpness.Level())
		for _, viewer := range p.tx.Viewers(target.Position()) {
			viewer.ViewEntityAction(target, entity.EnchantedHitAction{})
		}
	}
	if critical {
		damage *= 1.5
	}

	result := target.hurtByPlayerMelee(damage, baseDamage, p, force, verticalLimit)
	if result.accepted {
		if durable, ok := held.Item().(item.Durable); ok {
			main, off := p.HeldItems()
			p.SetHeldItems(p.damageItem(main, durable.DurabilityInfo().AttackDurability), off)
		}
	}

	p.SwingArm()
	p.tx.PlaySound(soundPos, sound.Attack{Damage: result.accepted})
	if !result.accepted {
		return false
	}
	if critical {
		for _, viewer := range p.tx.Viewers(target.Position()) {
			viewer.ViewEntityAction(target, entity.CriticalHitAction{})
		}
	}

	p.Exhaust(0.1)
	if fireAspect, ok := held.Enchantment(enchantment.FireAspect); ok {
		if flammable, ok := any(target).(entity.Flammable); ok {
			flammable.SetOnFire(enchantment.FireAspect.Duration(fireAspect.Level()))
		}
	}
	return true
}

type playerMeleeHurtResult struct {
	decision directMeleeDecision
	accepted bool
}

func (p *Player) hurtByPlayerMelee(damage, baseDamage float64, attacker *Player, force, verticalLimit float64) playerMeleeHurtResult {
	decision := p.directMelee.classify(baseDamage)
	if decision == directMeleeProtected || decision == directMeleeWarmRejected {
		return playerMeleeHurtResult{decision: decision}
	}
	result := p.hurt(damage, entity.AttackDamageSource{Attacker: attacker}, hurtOptions{
		continueThroughImmunity: true,
		deferHurtAction:         true,
	})
	if !result.policyAccepted {
		return playerMeleeHurtResult{decision: decision}
	}

	p.directMelee.commit(decision, baseDamage)
	if decision == directMeleeCold {
		if motion, ok := meleeKnockBackMotion(p.Position(), attacker.Position(), p.directMelee.motion, force, verticalLimit); ok {
			p.SetVelocity(motion)
		}
		if !p.Dead() {
			for _, viewer := range p.viewers() {
				viewer.ViewEntityAction(p, entity.HurtAction{})
			}
		}
	}
	return playerMeleeHurtResult{decision: decision, accepted: true}
}

// observeDamageForMelee keeps the victim-owned direct-melee gate shared across accepted damage sources. The source
// keeps its existing Dragonfly damage and motion behaviour.
func (p *Player) observeDamageForMelee(baseDamage float64, policyAccepted bool) {
	if !policyAccepted {
		return
	}
	decision := p.directMelee.classify(baseDamage)
	p.directMelee.commit(decision, baseDamage)
}
