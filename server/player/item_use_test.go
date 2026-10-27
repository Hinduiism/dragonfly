package player

import (
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestCancelledFoodUseSynchronisesBeforeImmediateRestart(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
		p.UseItem()
		// A stop action can arrive between two rapid right clicks. It must clear
		// the using flag on the client as well as on the server.
		h := &session.PlayerActionHandler{}
		if err := h.Handle(&packet.PlayerAction{
			EntityRuntimeID: 1, ActionType: protocol.PlayerActionStopItemUseOn,
		}, session.Nop, p.tx, p); err != nil {
			t.Fatal(err)
		}
		if p.UsingItem() {
			t.Fatal("cancelled food is still in use")
		}
		p.UseItem()
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.UseItem()
		held, _ := p.HeldItems()
		if held.Count() != 1 || p.Food() != 14 {
			t.Fatalf("restarted eating = count:%d food:%d, want count:1 food:14", held.Count(), p.Food())
		}
		assertItemUseStates(t, conn, []bool{true, false, true})
	})
}

func TestRapidFoodRestartsRequireFullConsumeDuration(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
		for range 3 {
			p.UseItem()
			p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
			p.ReleaseItem()
		}
		p.UseItem()
		p.UseItem()
		held, _ := p.HeldItems()
		if held.Count() != 2 || p.Food() != 10 {
			t.Fatalf("rapid clicks consumed food early: count:%d food:%d", held.Count(), p.Food())
		}
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.UseItem()
		held, _ = p.HeldItems()
		if held.Count() != 1 || p.Food() != 14 {
			t.Fatalf("completed eating = count:%d food:%d, want count:1 food:14", held.Count(), p.Food())
		}
		assertItemUseStates(t, conn, []bool{true, false, true, false, true, false, true})
	})
}

func TestCancelledBowReleaseSynchronisesStoppedUse(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.Handle(cancelItemRelease{})
		p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
		_ = p.Inventory().SetItem(1, item.NewStack(item.Arrow{}, 2))
		p.UseItem()
		p.usingSince = time.Now().Add(-time.Second)
		p.ReleaseItem()
		if p.UsingItem() {
			t.Fatal("cancelled bow release is still in use")
		}
		arrows, _ := p.Inventory().Item(1)
		if arrows.Count() != 2 {
			t.Fatal("cancelled bow release consumed an arrow")
		}
		assertItemUseStates(t, conn, []bool{true, false})
	})
}

type cancelItemRelease struct{ NopHandler }

func (cancelItemRelease) HandleItemRelease(ctx *Context, _ item.Stack, _ time.Duration) {
	ctx.Cancel()
}

func assertItemUseStates(t *testing.T, conn *stateConn, want []bool) {
	t.Helper()
	var got []bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got = got[:0]
		for _, pk := range conn.snapshot() {
			if update, ok := pk.(*packet.SetActorData); ok {
				got = append(got, update.EntityMetadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagUsingItem))
			}
		}
		if slices.Equal(got, want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client item-use states = %v, want %v", got, want)
}
