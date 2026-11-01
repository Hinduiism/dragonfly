package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// consumableUseState binds a meal to one world, slot and logical stack.
type consumableUseState struct {
	stack                item.Stack
	slot                 int
	world                *world.World
	revision, generation uint64
	committing, applying bool
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

func (p *Player) handleConsumableClick(food item.Consumable, held item.Stack) {
	if held.Empty() {
		return
	}
	if !p.usingItem || p.consumableUse.stack.Empty() {
		p.clearConsumableUse()
		p.consumableUse.stack, p.consumableUse.slot = held, int(*p.heldSlot)
		p.consumableUse.world = p.tx.World()
		p.consumableUse.revision, _ = p.inv.SlotRevision(int(*p.heldSlot))
		p.usingItem, p.usingSince = true, time.Now()
		p.updateState()
		return
	}
	p.consumeItem(food)
}

// consumeItem follows PM's completion-click model. No timer or pending retry
// can reject or outlive the input that requested this meal.
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
	useCtx := p.useContext()
	useCtx.CountSub, useCtx.NewItem = 1, food.Consume(p.tx, p)
	p.consumableUse.applying = true
	p.handleUseContext(useCtx)
	p.consumableUse.applying = false
	p.usingItem = false
	p.clearConsumableUse()
	p.updateState()
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
	// Recovery for abandoned input, independent of normal food completion.
	idle := min(time.Minute, max(10*time.Second, food.ConsumeDuration()+4*p.Latency()))
	if time.Since(p.usingSince) > idle {
		p.CancelItemUse()
	}
}
