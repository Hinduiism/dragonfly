package inventory

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func TestRevisionTracksAcceptedMutations(t *testing.T) {
	var callbacks atomic.Uint64
	inv := New(4, func(int, item.Stack, item.Stack) { callbacks.Add(1) })
	stick := item.NewStack(item.Stick{}, 1)
	diamond := item.NewStack(item.Diamond{}, 1)

	if got := inv.Revision(); got != 0 {
		t.Fatalf("initial revision = %d, want 0", got)
	}
	if err := inv.SetItem(0, stick); err != nil {
		t.Fatalf("SetItem() error = %v", err)
	}
	if got := inv.Revision(); got != 1 {
		t.Fatalf("revision after SetItem = %d, want 1", got)
	}
	if err := inv.SetItem(0, stick); err != nil {
		t.Fatalf("equivalent SetItem() error = %v", err)
	}
	if got := inv.Revision(); got != 2 {
		t.Fatalf("revision after equivalent write = %d, want 2", got)
	}
	if err := inv.SetItem(4, stick); err != ErrSlotOutOfRange {
		t.Fatalf("out-of-range SetItem() error = %v, want %v", err, ErrSlotOutOfRange)
	}
	if got := inv.Revision(); got != 2 {
		t.Fatalf("out-of-range write changed revision to %d", got)
	}

	if err := inv.SetItem(1, diamond); err != nil {
		t.Fatalf("second SetItem() error = %v", err)
	}
	beforeSwap := inv.Revision()
	if err := inv.Swap(0, 1); err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	if got := inv.Revision(); got != beforeSwap+2 {
		t.Fatalf("revision after Swap = %d, want %d", got, beforeSwap+2)
	}

	beforeAdd := inv.Revision()
	if _, err := inv.AddItem(item.NewStack(item.Coal{}, 2)); err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}
	if got := inv.Revision(); got <= beforeAdd {
		t.Fatalf("AddItem did not advance revision: before=%d after=%d", beforeAdd, got)
	}
	beforeRemove := inv.Revision()
	if err := inv.RemoveItem(item.NewStack(item.Coal{}, 1)); err != nil {
		t.Fatalf("RemoveItem() error = %v", err)
	}
	if got := inv.Revision(); got <= beforeRemove {
		t.Fatalf("RemoveItem did not advance revision: before=%d after=%d", beforeRemove, got)
	}

	beforeClear := inv.Revision()
	cleared := inv.Clear()
	if len(cleared) == 0 {
		t.Fatal("Clear() returned no stored items")
	}
	if got := inv.Revision(); got <= beforeClear {
		t.Fatalf("Clear did not advance revision: before=%d after=%d", beforeClear, got)
	}
	afterClear := inv.Revision()
	if cleared = inv.Clear(); len(cleared) != 0 {
		t.Fatalf("empty Clear() returned %d items", len(cleared))
	}
	if got := inv.Revision(); got != afterClear {
		t.Fatalf("empty Clear changed revision: got %d, want %d", got, afterClear)
	}
	if got := callbacks.Load(); got != inv.Revision() {
		t.Fatalf("slot callbacks = %d, revision = %d", got, inv.Revision())
	}
}

func TestRevisionIgnoresRejectedWrites(t *testing.T) {
	var callbacks atomic.Uint64
	inv := New(2, func(int, item.Stack, item.Stack) { callbacks.Add(1) })
	inv.SlotValidatorFunc(func(item.Stack, int) bool { return false })

	if err := inv.SetItem(0, item.NewStack(item.Stick{}, 1)); err != nil {
		t.Fatalf("SetItem() error = %v", err)
	}
	if got := inv.Revision(); got != 0 {
		t.Fatalf("rejected write revision = %d, want 0", got)
	}
	if got := callbacks.Load(); got != 0 {
		t.Fatalf("rejected write callbacks = %d, want 0", got)
	}
	if stack, err := inv.Item(0); err != nil || !stack.Empty() {
		t.Fatalf("rejected write stored item: stack=%v error=%v", stack, err)
	}
}

func TestCloneAndMergeStartIndependentRevisions(t *testing.T) {
	left := New(2, nil)
	right := New(1, nil)
	if err := left.SetItem(0, item.NewStack(item.Stick{}, 1)); err != nil {
		t.Fatal(err)
	}
	if err := right.SetItem(0, item.NewStack(item.Diamond{}, 1)); err != nil {
		t.Fatal(err)
	}

	clone := left.Clone(nil)
	if got := clone.Revision(); got != 0 {
		t.Fatalf("clone revision = %d, want 0", got)
	}
	merged := left.Merge(right, nil)
	if got := merged.Revision(); got != 0 {
		t.Fatalf("merged revision = %d, want 0", got)
	}
	if len(clone.Items()) != 1 || len(merged.Items()) != 2 {
		t.Fatalf("clone/merge contents changed: clone=%v merged=%v", clone.Items(), merged.Items())
	}
}

func TestRevisionConcurrentReadsAndWrites(t *testing.T) {
	const writes = 256
	inv := New(1, nil)
	stack := item.NewStack(item.Stick{}, 1)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range writes {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := inv.SetItem(0, stack); err != nil {
				t.Errorf("SetItem() error = %v", err)
			}
			_ = inv.Revision()
		}()
	}
	close(start)
	workers.Wait()
	if got := inv.Revision(); got != writes {
		t.Fatalf("revision = %d, want %d", got, writes)
	}
}

func BenchmarkRevisionedSetItem(b *testing.B) {
	inv := New(1, nil)
	stack := item.NewStack(item.Stick{}, 1)
	b.ReportAllocs()
	for range b.N {
		if err := inv.SetItem(0, stack); err != nil {
			b.Fatal(err)
		}
	}
}
