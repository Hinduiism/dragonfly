package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// MeleeCooldownHandler is optional. It is consulted on the attacker after
// ordinary attack authorization and only for normal direct player melee.
// Returning true authorizes one attack to use a fresh ordinary hit window.
// Cancellation still rejects the attack; hard protection is never bypassed.
type MeleeCooldownHandler interface {
	HandleMeleeCooldown(ctx *Context, target *Player) bool
}

// HeldSlotChangeObserver runs after a real held-slot assignment succeeds.
// It observes programmatic changes too; callers must guard their own loadouts.
type HeldSlotChangeObserver interface {
	HandleHeldSlotChanged(p *Player, from, to int)
}

// DamageOutcome is synchronous owner-scoped native observation data. Source
// may reference live entities: translate identities immediately, never queue it.
// Loss is actual removal, excluding overkill and totem replacement effects.
type DamageOutcome struct {
	Source                world.DamageSource
	HealthLost            float64
	AbsorptionLost        float64
	DirectMelee           bool
	Critical              bool
	MeleeCooldownOverride bool
	TotemUsed             bool
}

// DamageOutcomeHandler observes a normal policy-accepted damage operation,
// including one with zero actual loss. It is called once on the victim before
// death/retaliation. It cannot cancel or edit damage; observers must not move,
// heal, damage or replace players from this callback.
type DamageOutcomeHandler interface {
	HandleDamageOutcome(victim *Player, outcome DamageOutcome)
}

// ItemCooldownHandler optionally chooses the duration at native item-cooldown
// application. Nonpositive durations retain the item's native default.
type ItemCooldownHandler interface {
	HandleItemCooldown(p *Player, it world.Item, native time.Duration) time.Duration
}

func (p *Player) observeDamageOutcome(src world.DamageSource, opts hurtOptions, healthLost, absorptionLost float64, totem bool) {
	if h, ok := p.Handler().(DamageOutcomeHandler); ok {
		h.HandleDamageOutcome(p, DamageOutcome{
			Source: src, HealthLost: healthLost, AbsorptionLost: absorptionLost,
			DirectMelee: opts.directMelee, Critical: opts.critical,
			MeleeCooldownOverride: opts.freshMelee, TotemUsed: totem,
		})
	}
}
