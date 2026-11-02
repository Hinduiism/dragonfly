package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/item"
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
	lastActivity         time.Time
}

func (p *Player) clearConsumableUse() {
	p.consumableUse = consumableUseState{generation: p.consumableUse.generation + 1}
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

// UseItemWithTrigger retains the input origin, which is not a meal phase.
// Unknown can start use, and PlayerInput can finish an active meal.
func (p *Player) UseItemWithTrigger(trigger uint32) { p.useItem(trigger, true) }

// CanCompleteConsumableUse permits count-zero prediction for the last food
// only when the referenced slot still belongs to an active, valid meal.
func (p *Player) CanCompleteConsumableUse(slot int) bool {
	p.ValidateHeldItemUse()
	return p.usingItem && !p.consumableUse.stack.Empty() && p.consumableUse.slot == slot && !p.consumableUse.committing
}

func (p *Player) handleConsumableClick(food item.Consumable, held item.Stack, trigger uint32, fromClient bool) {
	if held.Empty() {
		return
	}
	if !p.usingItem || p.consumableUse.stack.Empty() {
		// A simulation-only repeat cannot begin a meal after cancellation.
		// Unknown describes neither a start nor a finish, so allow it to start.
		if fromClient && trigger != protocol.TriggerTypePlayerInput && trigger != protocol.TriggerTypeUnknown {
			p.rejectConsumableUse(held)
			return
		}
		p.clearConsumableUse()
		p.consumableUse.stack, p.consumableUse.slot = held, int(*p.heldSlot)
		p.consumableUse.world = p.tx.World()
		p.consumableUse.revision, _ = p.inv.SlotRevision(int(*p.heldSlot))
		p.consumableUse.lastActivity = time.Now()
		p.usingItem, p.usingSince = true, time.Now()
		p.updateState()
		return
	}
	if fromClient && trigger != protocol.TriggerTypePlayerInput && trigger != protocol.TriggerTypeSimulationTick && trigger != protocol.TriggerTypeUnknown {
		p.rejectConsumableUse(held)
		return
	}
	p.consumableUse.lastActivity = time.Now()
	p.consumableUse.completionRequested = true
	// Keep an early completion compressed by network jitter until the server's
	// use duration is met. Release or invalidation still cancels this request.
	if p.useDuration() < food.ConsumeDuration() {
		return
	}
	p.consumeItem(food)
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
	if p.consumableUse.completionRequested && p.useDuration() >= food.ConsumeDuration() {
		p.consumeItem(food)
		return
	}
	// Recovery for abandoned input, independent of normal food completion.
	idle := min(time.Minute, max(10*time.Second, food.ConsumeDuration()+4*p.Latency()))
	if time.Since(p.consumableUse.lastActivity) > idle {
		p.CancelItemUse()
	}
}
