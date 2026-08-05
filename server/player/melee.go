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
	pocketMineMeleeForce          = 0.4
	pocketMineMeleeVerticalLimit  = 0.4
	pocketMineAttackCooldownTicks = 10
	pocketMineSpawnProtection     = 60
	pocketMineMeleeReach          = 8.0
	pocketMineWaterEyeOffset      = 0.1111111
)

type pocketMineDamageDecision uint8

const (
	pocketMineDamageProtected pocketMineDamageDecision = iota
	pocketMineDamageCold
	pocketMineDamageWarmRejected
	pocketMineDamageWarmStronger
)

// pocketMineMeleeState holds only the transient state PocketMine uses to decide whether a direct melee hit
// produces motion. It is deliberately separate from Dragonfly's wall-clock health immunity.
type pocketMineMeleeState struct {
	attackTime      int64
	noDamageTicks   int64
	lastBaseDamage  float64
	motion          mgl64.Vec3
	motionSetTick   int64
	lastTick        int64
	tickInitialised bool
}

func newPocketMineMeleeState(spawnProtected bool) pocketMineMeleeState {
	state := pocketMineMeleeState{}
	if spawnProtected {
		state.noDamageTicks = pocketMineSpawnProtection
	}
	return state
}

func (state *pocketMineMeleeState) tick(current int64) {
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

func (state *pocketMineMeleeState) classify(baseDamage float64) pocketMineDamageDecision {
	if state.noDamageTicks > 0 {
		return pocketMineDamageProtected
	}
	if state.attackTime <= 0 {
		return pocketMineDamageCold
	}
	if state.lastBaseDamage >= baseDamage {
		return pocketMineDamageWarmRejected
	}
	return pocketMineDamageWarmStronger
}

func (state *pocketMineMeleeState) commit(decision pocketMineDamageDecision, baseDamage float64) {
	switch decision {
	case pocketMineDamageCold:
		state.attackTime = pocketMineAttackCooldownTicks
		state.lastBaseDamage = baseDamage
	case pocketMineDamageWarmStronger:
		state.lastBaseDamage = baseDamage
	}
}

func (state *pocketMineMeleeState) recordMotion(motion mgl64.Vec3, current int64) {
	state.motion, state.motionSetTick = motion, current
}

func (state *pocketMineMeleeState) recordJump(vertical float64, current int64) {
	state.motion[1], state.motionSetTick = vertical, current
}

func (state *pocketMineMeleeState) clearMotion(current int64) {
	state.motion, state.motionSetTick = mgl64.Vec3{}, current
}

func (state *pocketMineMeleeState) resumeAt(current int64) {
	state.lastTick, state.tickInitialised = current, true
}

// pocketMineMeleeMotion reproduces Living::knockBack() from the pinned PocketMine source. Keep the
// reciprocal and multiplication operations in this order so protocol float conversion starts from the same value.
func pocketMineMeleeMotion(victim, attacker, previous mgl64.Vec3, force, verticalLimit float64) (mgl64.Vec3, bool) {
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

func (p *Player) canPocketMineMeleeInteract(target *Player) bool {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return false
	}
	eye, targetPos := entity.EyePosition(p), target.Position()
	if eye.Sub(targetPos).LenSqr() > pocketMineMeleeReach*pocketMineMeleeReach {
		return false
	}
	direction := p.Rotation().Vec3()
	return direction.Dot(targetPos)-direction.Dot(eye) >= -math.Sqrt(3)/2
}

func (p *Player) pocketMineMeleeCritical() bool {
	_, blind := p.Effect(effect.Blindness)
	return !p.Sprinting() && !p.Flying() && p.FallDistance() > 0 && !blind && !p.pocketMineMeleeUnderwater()
}

func (p *Player) pocketMineMeleeUnderwater() bool {
	eye := entity.EyePosition(p)
	pos := cube.PosFromVec3(eye)
	liquid, ok := p.tx.Liquid(pos)
	if !ok {
		return false
	}
	if _, ok := liquid.(block.Water); !ok {
		return false
	}
	return eye[1] < pocketMineWaterSurface(float64(pos[1]), liquid.LiquidDepth(), liquid.LiquidFalling())
}

// pocketMineWaterSurface converts Dragonfly's depth representation to PocketMine's decay representation before
// applying Entity::isUnderwater(). A source or falling block has decay 0; progressively shallower water has 1-7.
func pocketMineWaterSurface(blockY float64, depth int, falling bool) float64 {
	decay := 8 - depth
	if falling {
		decay = 0
	}
	fluidHeightPercent := float64(decay+1) / 9
	return blockY + 1 - (fluidHeightPercent - pocketMineWaterEyeOffset)
}

func (p *Player) pocketMineMeleeSoundPosition(target *Player) mgl64.Vec3 {
	return target.Position().Add(mgl64.Vec3{0, Type.BBox(target).Height() / 2})
}

// attackPlayer handles the scoped direct player-versus-player path. Other AttackEntity targets remain on
// Dragonfly's normal combat implementation.
func (p *Player) attackPlayer(target *Player) bool {
	if target == p || target.Dead() {
		return false
	}
	soundPos := p.pocketMineMeleeSoundPosition(target)
	if !p.canPocketMineMeleeInteract(target) {
		p.SwingArm()
		p.tx.PlaySound(soundPos, sound.Attack{})
		return false
	}

	force, verticalLimit := pocketMineMeleeForce, pocketMineMeleeVerticalLimit
	critical := p.pocketMineMeleeCritical()
	ctx := newContext(p)
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
	decision pocketMineDamageDecision
	accepted bool
}

func (p *Player) hurtByPlayerMelee(damage, baseDamage float64, attacker *Player, force, verticalLimit float64) playerMeleeHurtResult {
	decision := p.pocketMineMelee.classify(baseDamage)
	if decision == pocketMineDamageProtected || decision == pocketMineDamageWarmRejected {
		return playerMeleeHurtResult{decision: decision}
	}
	result := p.hurt(damage, entity.AttackDamageSource{Attacker: attacker}, hurtOptions{
		continueThroughImmunity: true,
		deferHurtAction:         true,
	})
	if !result.policyAccepted {
		return playerMeleeHurtResult{decision: decision}
	}

	p.pocketMineMelee.commit(decision, baseDamage)
	if decision == pocketMineDamageCold {
		if motion, ok := pocketMineMeleeMotion(p.Position(), attacker.Position(), p.pocketMineMelee.motion, force, verticalLimit); ok {
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

// Observe accepted non-melee damage so PocketMine's victim-owned attack gate remains shared across damage sources.
// The source keeps its existing Dragonfly damage and motion behaviour.
func (p *Player) observePocketMineDamage(baseDamage float64, policyAccepted bool) {
	if !policyAccepted {
		return
	}
	decision := p.pocketMineMelee.classify(baseDamage)
	p.pocketMineMelee.commit(decision, baseDamage)
}
