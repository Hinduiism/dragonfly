package player

import (
	"errors"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// CombatState is an opaque, copy-owned, runtime-only snapshot. Inventory,
// custody, role, world, return eligibility and one-use consumption are caller
// responsibilities. Capture/restore require the current player owner.
type CombatState struct {
	valid                       bool
	owner                       uuid.UUID
	health, baseMax, absorption float64
	food, foodTick              int
	saturation, exhaustion      float64
	experience                  int
	effects                     []effect.Effect
	cooldowns                   map[string]time.Time
	direct, motion              combatGate
	immuneUntil                 time.Time
	lastDamage                  float64
	projectileImmuneUntil       time.Time
	projectileLastDamage        float64
	motionProjectileUntil       time.Time
	motionProjectileDamage      float64
	fireUntil                   time.Time
	air, maxAir                 int
}

type combatGate struct {
	attackUntil, protectedUntil time.Time
	lastBaseDamage              float64
}

// CaptureCombatState copies native combat transients without inventory/live
// pointers. Effects/hunger keep their remaining state; cooldowns are absolute.
func (p *Player) CaptureCombatState() (CombatState, error) {
	return p.captureCombatState(time.Now())
}

func (p *Player) captureCombatState(now time.Time) (CombatState, error) {
	if p == nil || p.handle == nil || p.handle.Closed() || p.Dead() {
		return CombatState{}, errors.New("player: cannot capture dead or closed combat state")
	}
	s := CombatState{
		valid: true, owner: p.UUID(), health: p.Health(), baseMax: p.MaxHealth(), absorption: p.Absorption(),
		experience: p.Experience(), effects: append([]effect.Effect(nil), p.Effects()...),
		cooldowns:   make(map[string]time.Time, len(p.cooldowns)),
		direct:      captureCombatGate(p.directMelee, p.tx.CurrentTick(), now),
		motion:      captureCombatGate(p.motionMelee, p.tx.CurrentTick(), now),
		immuneUntil: p.immuneUntil, lastDamage: p.lastDamage,
		projectileImmuneUntil: p.projectileImmuneUntil, projectileLastDamage: p.projectileLastDamage,
		motionProjectileUntil: p.motionProjectileUntil, motionProjectileDamage: p.motionProjectileDamage,
		air: p.airSupplyTicks, maxAir: p.maxAirSupplyTicks,
	}
	if boost, ok := p.Effect(effect.HealthBoost); ok {
		s.baseMax -= 4 * float64(boost.Level())
	}
	if s.baseMax <= 0 || math.IsNaN(s.baseMax) || math.IsInf(s.baseMax, 0) {
		return CombatState{}, errors.New("player: invalid base maximum health")
	}
	p.hunger.mu.RLock()
	s.food, s.foodTick = p.hunger.foodLevel, p.hunger.foodTick
	s.saturation, s.exhaustion = p.hunger.saturationLevel, p.hunger.exhaustionLevel
	p.hunger.mu.RUnlock()
	for name, until := range p.cooldowns {
		s.cooldowns[name] = until
	}
	if p.fireTicks > 0 {
		s.fireUntil = now.Add(time.Duration(p.fireTicks) * time.Second / 20)
	}
	return s, nil
}

func captureCombatGate(g directMeleeState, tick int64, now time.Time) combatGate {
	if g.tickInitialised {
		g.tick(tick)
	}
	return combatGate{
		attackUntil:    now.Add(time.Duration(g.attackTime) * time.Second / 20),
		protectedUntil: now.Add(time.Duration(g.noDamageTicks) * time.Second / 20),
		lastBaseDamage: g.lastBaseDamage,
	}
}

// RestoreCombatState restores a live same-account player after caller-checked
// arrival. It replaces fresh reconnect protection with the source deadlines,
// never grants a new protection window and does not change equipment or mode.
func (p *Player) RestoreCombatState(s CombatState) error {
	return p.restoreCombatState(s, time.Now())
}

func (p *Player) restoreCombatState(s CombatState, now time.Time) error {
	if !s.valid || p == nil || p.handle == nil || p.handle.Closed() || p.Dead() || p.UUID() != s.owner {
		return errors.New("player: invalid combat state or target")
	}
	for _, e := range p.Effects() {
		p.RemoveEffect(e.Type())
	}
	p.SetMaxHealth(s.baseMax)
	for _, e := range s.effects {
		p.AddEffect(e)
	}
	if err := p.SetHealth(s.health); err != nil {
		return err
	}
	// Effect.Start may fill absorption. Restore current remaining hearts last.
	p.SetAbsorption(s.absorption)
	p.hunger.mu.Lock()
	p.hunger.foodLevel, p.hunger.foodTick = s.food, s.foodTick
	p.hunger.saturationLevel, p.hunger.exhaustionLevel = s.saturation, s.exhaustion
	p.hunger.mu.Unlock()
	p.sendFood()
	p.AddExperience(s.experience - p.Experience())
	p.cooldowns = make(map[string]time.Time, len(s.cooldowns))
	for name, until := range s.cooldowns {
		if until.After(now) {
			p.cooldowns[name] = until
			if it, ok := world.ItemByName(name, 0); ok {
				p.session().ViewItemCooldown(it, until.Sub(now))
			}
		}
	}
	p.directMelee = restoreCombatGate(s.direct, p.tx.CurrentTick(), now)
	p.motionMelee = restoreCombatGate(s.motion, p.tx.CurrentTick(), now)
	p.immuneUntil, p.lastDamage = s.immuneUntil, s.lastDamage
	p.projectileImmuneUntil, p.projectileLastDamage = s.projectileImmuneUntil, s.projectileLastDamage
	p.motionProjectileUntil, p.motionProjectileDamage = s.motionProjectileUntil, s.motionProjectileDamage
	p.fireTicks = combatRemainingTicks(s.fireUntil, now)
	p.airSupplyTicks, p.maxAirSupplyTicks = s.air, s.maxAir
	p.CancelItemUse()
	p.ResetFallDistance()
	p.updateState()
	return nil
}

func restoreCombatGate(g combatGate, tick int64, now time.Time) directMeleeState {
	return directMeleeState{
		attackTime:     combatRemainingTicks(g.attackUntil, now),
		noDamageTicks:  combatRemainingTicks(g.protectedUntil, now),
		lastBaseDamage: g.lastBaseDamage,
		lastTick:       tick, tickInitialised: true,
	}
}

func combatRemainingTicks(until, now time.Time) int64 {
	d := until.Sub(now)
	if d <= 0 {
		return 0
	}
	const tick = time.Second / 20
	return int64((d + tick - 1) / tick)
}
