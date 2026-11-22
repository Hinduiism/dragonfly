package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// consumableUseState binds a meal to one world, slot and logical stack.
type consumableUseState struct {
	stack                item.Stack
	slot                 int
	world                *world.World
	revision, generation uint64
	committing, applying bool
	completionRequested  bool
	finishWithStop       bool
	restart              bool
	lastActivity         time.Time
}

// closedConsumableUse prevents stray finish inputs from starting a second meal.
type closedConsumableUse struct {
	world    *world.World
	slot     int
	revision uint64
	released bool
}

// Cap the wait after a paired finish and stop. The full use timer still applies.
const consumableFinishGrace = 100 * time.Millisecond

func (p *Player) clearConsumableUse() {
	use := p.consumableUse
	if !use.stack.Empty() {
		p.closedConsumable = closedConsumableUse{world: use.world, slot: use.slot, revision: use.revision}
	}
	p.consumableUse = consumableUseState{generation: use.generation + 1}
}

// StopItemUse handles a client release. A pending meal that has reached its
// deadline can finish here, even if its world tick has not run yet.
func (p *Player) StopItemUse(paired bool) {
	p.ValidateHeldItemUse()
	use := &p.consumableUse
	if !paired {
		use.restart = false
	}
	if use.stack.Empty() || !p.finishConsumableIfReady(use.stack.Item().(item.Consumable)) {
		if use.finishWithStop {
			return
		}
		p.CancelItemUse()
	}
	if !paired && p.closedConsumable.world == p.tx.World() && p.closedConsumable.slot == int(*p.heldSlot) {
		p.closedConsumable.released = true
	}
}

// CancelItemUse stops use without firing a bow or completing a meal. Actual bow
// release transactions continue through ReleaseItem and its charge-time checks.
func (p *Player) CancelItemUse() {
	foodUse, wasUsing := !p.consumableUse.stack.Empty(), p.usingItem
	p.usingItem = false
	p.clearConsumableUse()
	if foodUse {
		p.synchronizeFoodUse()
	} else if wasUsing {
		p.updateState()
	}
}

func (p *Player) synchronizeFoodUse() {
	p.sendFood()
	p.session().ResendHeldItem()
	p.updateState()
}

func (p *Player) rejectConsumableUse(held item.Stack) {
	if _, ok := held.Item().(item.Consumable); ok {
		p.usingItem = false
		p.clearConsumableUse()
		// A rejected first click may already have been predicted locally.
		p.synchronizeFoodUse()
	}
}

func (p *Player) canConsume(food item.Consumable) bool {
	if c, ok := food.(interface{ CanConsume() bool }); ok && !c.CanConsume() {
		return false
	}
	return food.AlwaysConsumable() || !p.GameMode().AllowsTakingDamage() || p.Food() < 20 || p.tx.World().Difficulty().FoodRegenerates()
}

// ValidateHeldItemUse permits additive pickup while rejecting replacements,
// slot switches and world changes, without depending on network stack IDs.
func (p *Player) ValidateHeldItemUse() {
	use := p.consumableUse
	if !p.usingItem || use.stack.Empty() || use.applying {
		return
	}
	held, _ := p.HeldItems()
	revision, err := p.inv.SlotRevision(int(*p.heldSlot))
	if p.Dead() || !p.GameMode().AllowsInteraction() || use.world != p.tx.World() ||
		int(*p.heldSlot) != use.slot || held.Empty() || err != nil || revision != use.revision || !use.stack.Comparable(held) {
		p.CancelItemUse()
		return
	}
	p.consumableUse.stack = held
	if food, ok := held.Item().(item.Consumable); ok && !p.canConsume(food) {
		p.CancelItemUse()
	}
}

// UseItemWithTrigger handles a use transaction without a separate start signal.
func (p *Player) UseItemWithTrigger(trigger uint32) {
	p.UseItemFromClient(session.ItemUseInput{Trigger: trigger})
}

// UseItemFromClient preserves the input frame's start and stop signals.
func (p *Player) UseItemFromClient(input session.ItemUseInput) { p.useItem(input, true) }

// StartUsingConsumable handles a start signal without an item transaction.
func (p *Player) StartUsingConsumable() {
	held, _ := p.HeldItems()
	if _, ok := held.Item().(item.Consumable); ok {
		p.UseItemFromClient(session.ItemUseInput{Start: true})
	}
}

// CanCompleteConsumableUse permits count-zero prediction for the last food
// only when the referenced slot still belongs to an active, valid meal.
func (p *Player) CanCompleteConsumableUse(slot int) bool {
	p.ValidateHeldItemUse()
	return p.usingItem && !p.consumableUse.stack.Empty() && p.consumableUse.slot == slot && !p.consumableUse.committing
}

func (p *Player) handleConsumableClick(food item.Consumable, held item.Stack, input session.ItemUseInput, fromClient bool) {
	if held.Empty() {
		return
	}
	if !p.usingItem || p.consumableUse.stack.Empty() {
		slot := int(*p.heldSlot)
		revision, _ := p.inv.SlotRevision(slot)
		if fromClient && !p.canStartConsumable(input, slot, revision) {
			p.rejectConsumableUse(held)
			return
		}
		p.clearConsumableUse()
		p.closedConsumable = closedConsumableUse{}
		p.consumableUse.stack, p.consumableUse.slot = held, slot
		p.consumableUse.world = p.tx.World()
		p.consumableUse.revision = revision
		p.consumableUse.lastActivity = time.Now()
		p.usingItem, p.usingSince = true, time.Now()
		p.updateState()
		return
	}
	if input.Start {
		if p.consumableUse.finishWithStop {
			p.consumableUse.restart = true
			p.finishConsumableIfReady(food)
		}
		return
	}
	if fromClient && input.Trigger != protocol.TriggerTypePlayerInput && input.Trigger != protocol.TriggerTypeSimulationTick && input.Trigger != protocol.TriggerTypeUnknown {
		p.rejectConsumableUse(held)
		return
	}
	if input.Stop && food.ConsumeDuration()-p.useDuration() <= consumableFinishGrace {
		p.consumableUse.finishWithStop = true
	}
	p.consumableUse.lastActivity = time.Now()
	p.consumableUse.completionRequested = true
	p.finishConsumableIfReady(food)
}

func (p *Player) canStartConsumable(input session.ItemUseInput, slot int, revision uint64) bool {
	if input.Start {
		return true
	}
	if input.Stop || (input.Trigger != protocol.TriggerTypeUnknown && input.Trigger != protocol.TriggerTypePlayerInput) {
		return false
	}
	closed := p.closedConsumable
	// A standalone press can use the newly acknowledged stack after a meal
	// or follow a release. The previous stack's finish is still ambiguous.
	if closed.world != p.tx.World() || closed.slot != slot {
		return true
	}
	if matches, ok := input.StackIDMatches.Value(); ok && closed.revision != revision {
		return matches
	}
	return closed.released
}

func (p *Player) finishConsumableIfReady(food item.Consumable) bool {
	if !p.consumableUse.completionRequested || p.useDuration() < food.ConsumeDuration() {
		return false
	}
	p.consumeItem(food)
	return true
}

// consumeItem applies a validated meal once, against its original inventory.
func (p *Player) consumeItem(food item.Consumable) {
	p.ValidateHeldItemUse()
	if !p.usingItem || p.consumableUse.stack.Empty() || p.consumableUse.committing {
		return
	}
	held, _ := p.HeldItems()
	if !p.canConsume(food) {
		p.CancelItemUse()
		return
	}
	use := p.consumableUse
	if use.restart {
		// The next press may arrive while a paired finish still has time left.
		// Recheck after the old bite, including handler-owned consumption.
		defer func() {
			if p.usingItem || p.Dead() || p.tx.Closed() || p.tx.World() != use.world || int(*p.heldSlot) != use.slot {
				return
			}
			if remaining, _ := p.HeldItems(); !remaining.Empty() && remaining.Comparable(held) {
				p.StartUsingConsumable()
			}
		}()
	}
	p.consumableUse.committing = true
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemConsume(ctx, held); ctx.Cancelled() {
		if p.consumableUse.generation == use.generation {
			p.CancelItemUse()
		}
		return
	}
	if p.consumableUse.generation != use.generation {
		return
	}
	p.ValidateHeldItemUse()
	current, _ := p.HeldItems()
	revision, _ := p.inv.SlotRevision(int(*p.heldSlot))
	if !p.usingItem || p.Dead() || !p.GameMode().AllowsInteraction() || use.world != p.tx.World() ||
		p.consumableUse.generation != use.generation || int(*p.heldSlot) != use.slot || revision != use.revision || current.Empty() || !current.Comparable(held) {
		p.CancelItemUse()
		return
	}
	// Effects may kill the player or replace their inventory. Deduct from the
	// validated stack before applying them, never from the later inventory.
	p.consumableUse.applying = true
	_, offHand := p.HeldItems()
	p.SetHeldItems(p.subtractItem(current, 1), offHand)
	p.usingItem = false
	p.clearConsumableUse()
	p.updateState()
	residue := food.Consume(p.tx, p)
	if p.Dead() || p.tx.Closed() || p.tx.World() != use.world || !p.GameMode().AllowsInteraction() {
		return
	}
	useCtx := p.useContext()
	useCtx.NewItem = residue
	p.addNewItem(useCtx)
	p.tx.PlaySound(p.Position().Add(mgl64.Vec3{0, 1.5}), sound.Burp{})
}

func (p *Player) tickConsumableUse() {
	p.ValidateHeldItemUse()
	if !p.usingItem || p.consumableUse.stack.Empty() {
		return
	}
	held, _ := p.HeldItems()
	food, ok := held.Item().(item.Consumable)
	if !ok {
		p.CancelItemUse()
		return
	}
	if p.finishConsumableIfReady(food) {
		return
	}
	// Recovery for abandoned input, independent of normal food completion.
	idle := min(time.Minute, max(10*time.Second, food.ConsumeDuration()+4*p.Latency()))
	if time.Since(p.consumableUse.lastActivity) > idle {
		p.CancelItemUse()
	}
}
