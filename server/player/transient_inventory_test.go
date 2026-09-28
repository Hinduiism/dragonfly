package player

import (
	"errors"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestTransientInventoryTakeAndRestore(t *testing.T) {
	withTransientInventoryPlayer(t, func(tx *world.Tx, p *Player) {
		cursor := item.NewStack(item.Diamond{}, 3)
		result := item.NewStack(item.Apple{}, 2)
		if err := p.ui.SetItem(0, cursor); err != nil {
			t.Fatal(err)
		}
		if err := p.ui.SetItem(50, result); err != nil {
			t.Fatal(err)
		}

		captured := p.SnapshotTransientInventory()
		if captured.Revision() != p.ui.Revision() || captured.Empty() {
			t.Fatalf("capture = revision %d empty %t", captured.Revision(), captured.Empty())
		}
		if got := captured.Slots(); !got[0].Equal(cursor) || !got[50].Equal(result) {
			t.Fatalf("captured slots = %#v", got)
		}

		before := p.ui.Revision()
		if _, _, err := p.TakeTransientInventory(before + 1); !errors.Is(err, ErrTransientInventoryChanged) {
			t.Fatalf("stale take error = %v, want ErrTransientInventoryChanged", err)
		}
		if p.ui.Revision() != before || p.ui.Empty() {
			t.Fatal("stale take changed transient storage")
		}

		custody, emptyRevision, err := p.TakeTransientInventory(before)
		if err != nil {
			t.Fatalf("TakeTransientInventory() error = %v", err)
		}
		if !p.ui.Empty() || emptyRevision != before+2 {
			t.Fatalf("take result: empty=%t revision=%d, want %d", p.ui.Empty(), emptyRevision, before+2)
		}
		if got := custody.Slots(); !got[0].Equal(cursor) || !got[50].Equal(result) {
			t.Fatalf("custody slots = %#v", got)
		}

		if _, err = p.RestoreTransientInventory(custody, emptyRevision+1); !errors.Is(err, ErrTransientInventoryChanged) {
			t.Fatalf("stale restore error = %v, want ErrTransientInventoryChanged", err)
		}
		if !p.ui.Empty() {
			t.Fatal("stale restore changed transient storage")
		}

		restoredRevision, err := p.RestoreTransientInventory(custody, emptyRevision)
		if err != nil {
			t.Fatalf("RestoreTransientInventory() error = %v", err)
		}
		if restoredRevision != emptyRevision+2 {
			t.Fatalf("restored revision = %d, want %d", restoredRevision, emptyRevision+2)
		}
		if got, _ := p.ui.Item(0); !got.Equal(cursor) {
			t.Fatalf("restored cursor = %v, want %v", got, cursor)
		}
		if got, _ := p.ui.Item(50); !got.Equal(result) {
			t.Fatalf("restored result = %v, want %v", got, result)
		}
		if _, err = p.RestoreTransientInventory(custody, restoredRevision); !errors.Is(err, ErrTransientInventoryRestored) {
			t.Fatalf("second restore error = %v, want ErrTransientInventoryRestored", err)
		}
	})
}

func TestTransientInventoryTakePreventsOverflowDrops(t *testing.T) {
	withTransientInventoryPlayer(t, func(tx *world.Tx, p *Player) {
		full := item.NewStack(item.Stick{}, 64)
		for slot := range p.Inventory().Size() {
			if err := p.Inventory().SetItem(slot, full); err != nil {
				t.Fatal(err)
			}
		}
		cursor := item.NewStack(item.Diamond{}, 1)
		if err := p.ui.SetItem(0, cursor); err != nil {
			t.Fatal(err)
		}
		beforeEntities := entityCount(tx)
		custody, _, err := p.TakeTransientInventory(p.ui.Revision())
		if err != nil {
			t.Fatal(err)
		}
		p.MoveItemsToInventory()
		if got := entityCount(tx); got != beforeEntities {
			t.Fatalf("settlement spawned %d entities, want %d", got, beforeEntities)
		}
		if custody.Empty() || !p.ui.Empty() {
			t.Fatalf("take custody empty=%t ui empty=%t", custody.Empty(), p.ui.Empty())
		}
		if got := p.Inventory().Items(); len(got) != p.Inventory().Size() {
			t.Fatalf("main inventory contains %d stacks, want %d", len(got), p.Inventory().Size())
		}
	})
}

func TestMoveItemsToInventoryStillMovesAndDropsOverflow(t *testing.T) {
	withTransientInventoryPlayer(t, func(tx *world.Tx, p *Player) {
		moved := item.NewStack(item.Diamond{}, 1)
		if err := p.ui.SetItem(0, moved); err != nil {
			t.Fatal(err)
		}
		p.MoveItemsToInventory()
		if got, _ := p.Inventory().Item(0); !got.Equal(moved) {
			t.Fatalf("settled main slot = %v, want %v", got, moved)
		}
		if !p.ui.Empty() {
			t.Fatal("settlement retained transient items")
		}

		full := item.NewStack(item.Stick{}, 64)
		for slot := range p.Inventory().Size() {
			if err := p.Inventory().SetItem(slot, full); err != nil {
				t.Fatal(err)
			}
		}
		overflow := item.NewStack(item.Apple{}, 1)
		if err := p.ui.SetItem(0, overflow); err != nil {
			t.Fatal(err)
		}
		before := entityCount(tx)
		p.MoveItemsToInventory()
		if got := entityCount(tx); got != before+1 {
			t.Fatalf("overflow settlement entity count = %d, want %d", got, before+1)
		}
		if !p.ui.Empty() {
			t.Fatal("overflow settlement retained transient items")
		}
	})
}

func TestTransientInventoryRestoreValidationDoesNotConsumeCustody(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })
	err := runtime.Do(func(tx *world.Tx) {
		first := addTransientInventoryPlayer(tx, "First")
		second := addTransientInventoryPlayer(tx, "Second")
		original := item.NewStack(item.Diamond{}, 1)
		if err := first.ui.SetItem(4, original); err != nil {
			t.Fatal(err)
		}
		custody, emptyRevision, err := first.TakeTransientInventory(first.ui.Revision())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = second.RestoreTransientInventory(custody, second.ui.Revision()); !errors.Is(err, ErrTransientInventorySnapshot) {
			t.Fatalf("other-player restore error = %v, want ErrTransientInventorySnapshot", err)
		}

		occupied := item.NewStack(item.Apple{}, 1)
		if err = first.ui.SetItem(2, occupied); err != nil {
			t.Fatal(err)
		}
		occupiedRevision := first.ui.Revision()
		if _, err = first.RestoreTransientInventory(custody, occupiedRevision); !errors.Is(err, ErrTransientInventoryOccupied) {
			t.Fatalf("occupied restore error = %v, want ErrTransientInventoryOccupied", err)
		}
		first.ui.Clear()
		if _, err = first.RestoreTransientInventory(custody, first.ui.Revision()); err != nil {
			t.Fatalf("restore after validation failures = %v", err)
		}
		if got, _ := first.ui.Item(4); !got.Equal(original) {
			t.Fatalf("restored item = %v, want %v (take revision %d)", got, original, emptyRevision)
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransientInventorySettlementInvalidatesRestore(t *testing.T) {
	withTransientInventoryPlayer(t, func(_ *world.Tx, p *Player) {
		if err := p.ui.SetItem(0, item.NewStack(item.Diamond{}, 1)); err != nil {
			t.Fatal(err)
		}
		custody, revision, err := p.TakeTransientInventory(p.ui.Revision())
		if err != nil {
			t.Fatal(err)
		}
		p.MoveItemsToInventory()
		if _, err = p.RestoreTransientInventory(custody, revision); !errors.Is(err, ErrTransientInventoryContextChanged) {
			t.Fatalf("restore after settlement error = %v, want ErrTransientInventoryContextChanged", err)
		}
		if !p.ui.Empty() {
			t.Fatal("restore after settlement changed transient storage")
		}
	})
}

func TestTransientInventoryCustodyRestoresAtMostOnce(t *testing.T) {
	withTransientInventoryPlayer(t, func(_ *world.Tx, p *Player) {
		if err := p.ui.SetItem(0, item.NewStack(item.Diamond{}, 1)); err != nil {
			t.Fatal(err)
		}
		custody, revision, err := p.TakeTransientInventory(p.ui.Revision())
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		var workers sync.WaitGroup
		for range 2 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				_, restoreErr := p.RestoreTransientInventory(custody, revision)
				errs <- restoreErr
			}()
		}
		close(start)
		workers.Wait()
		close(errs)
		var successes int
		for err := range errs {
			if err == nil {
				successes++
				continue
			}
			if !errors.Is(err, ErrTransientInventoryRestored) && !errors.Is(err, ErrTransientInventoryChanged) && !errors.Is(err, ErrTransientInventoryOccupied) {
				t.Fatalf("concurrent restore error = %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful restores = %d, want 1", successes)
		}
	})
}

func withTransientInventoryPlayer(t *testing.T, run func(*world.Tx, *Player)) {
	t.Helper()
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })
	err := runtime.Do(func(tx *world.Tx) { run(tx, addTransientInventoryPlayer(tx, "Transient Test")) }).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func addTransientInventoryPlayer(tx *world.Tx, name string) *Player {
	handle := world.EntitySpawnOpts{}.New(Type, Config{Name: name})
	return tx.AddEntity(handle).(*Player)
}

func entityCount(tx *world.Tx) int {
	var count int
	for range tx.Entities() {
		count++
	}
	return count
}
