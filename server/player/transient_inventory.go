package player

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
)

var (
	// ErrTransientInventoryChanged is returned when the transient inventory no
	// longer has the expected revision.
	ErrTransientInventoryChanged = errors.New("transient inventory changed")
	// ErrTransientInventoryOccupied is returned when a restore would overwrite
	// items currently held in transient storage.
	ErrTransientInventoryOccupied = errors.New("transient inventory is not empty")
	// ErrTransientInventorySnapshot is returned when a snapshot was not taken
	// from the current Player or was not produced by TakeTransientInventory.
	ErrTransientInventorySnapshot = errors.New("invalid transient inventory snapshot")
	// ErrTransientInventoryRestored is returned when custody represented by a
	// snapshot was already restored.
	ErrTransientInventoryRestored = errors.New("transient inventory snapshot was already restored")
	// ErrTransientInventoryContextChanged is returned when normal container
	// settlement ran after custody was taken.
	ErrTransientInventoryContextChanged = errors.New("transient inventory context changed")
)

// TransientInventorySnapshot is a copied view of the Player's cursor,
// crafting and workstation slots at one inventory revision. Slots returns a
// copy; snapshots do not expose the underlying inventory.
type TransientInventorySnapshot struct {
	slots    []item.Stack
	revision uint64
	owner    *inventory.Inventory
	restored *atomic.Bool
	epoch    uint64
}

// Slots returns a copy of every transient slot, including empty slots.
func (s TransientInventorySnapshot) Slots() []item.Stack { return slices.Clone(s.slots) }

// Revision returns the transient inventory revision captured by the snapshot.
func (s TransientInventorySnapshot) Revision() uint64 { return s.revision }

// Empty reports whether every captured transient slot is empty.
func (s TransientInventorySnapshot) Empty() bool {
	for _, stack := range s.slots {
		if !stack.Empty() {
			return false
		}
	}
	return true
}

// SnapshotTransientInventory copies the Player's current cursor, crafting and
// workstation storage. This includes committed result slots, but not recipe
// previews or pending results that have not entered the UI inventory. It must
// be called on the Player's current world owner at a completed item-transaction
// boundary.
func (p *Player) SnapshotTransientInventory() TransientInventorySnapshot {
	return TransientInventorySnapshot{
		slots:    p.ui.Slots(),
		revision: p.ui.Revision(),
		owner:    p.ui,
		epoch:    p.transientEpoch,
	}
}

// TakeTransientInventory transfers custody of all committed cursor, crafting
// and workstation items to a copied snapshot without moving them to the main
// inventory or dropping overflow. The returned revision is the empty
// transient inventory's revision after the take.
//
// It must be called on the Player's current world owner at a completed item-
// transaction boundary. A revision mismatch leaves all slots unchanged.
func (p *Player) TakeTransientInventory(expectedRevision uint64) (TransientInventorySnapshot, uint64, error) {
	if p.ui.Revision() != expectedRevision {
		return TransientInventorySnapshot{}, p.ui.Revision(), ErrTransientInventoryChanged
	}
	snapshot := TransientInventorySnapshot{
		slots:    p.ui.Slots(),
		revision: expectedRevision,
		owner:    p.ui,
		restored: new(atomic.Bool),
		epoch:    p.transientEpoch,
	}
	if p.ui.Revision() != expectedRevision {
		return TransientInventorySnapshot{}, p.ui.Revision(), ErrTransientInventoryChanged
	}
	p.ui.Clear()
	return snapshot, p.ui.Revision(), nil
}

// RestoreTransientInventory restores custody previously returned by
// TakeTransientInventory into the same Player's empty transient storage. The
// returned revision is the inventory revision after restoration.
//
// It must be called on the Player's current world owner at a completed item-
// transaction boundary. Validation failures do not consume the snapshot.
func (p *Player) RestoreTransientInventory(snapshot TransientInventorySnapshot, expectedRevision uint64) (uint64, error) {
	if snapshot.owner == nil || snapshot.owner != p.ui || snapshot.restored == nil || len(snapshot.slots) != p.ui.Size() {
		return p.ui.Revision(), ErrTransientInventorySnapshot
	}
	if snapshot.restored.Load() {
		return p.ui.Revision(), ErrTransientInventoryRestored
	}
	if snapshot.epoch != p.transientEpoch {
		return p.ui.Revision(), ErrTransientInventoryContextChanged
	}
	if revision := p.ui.Revision(); revision != expectedRevision {
		return revision, ErrTransientInventoryChanged
	}
	if !p.ui.Empty() {
		return p.ui.Revision(), ErrTransientInventoryOccupied
	}
	if !snapshot.restored.CompareAndSwap(false, true) {
		return p.ui.Revision(), ErrTransientInventoryRestored
	}
	for slot, stack := range snapshot.slots {
		if stack.Empty() {
			continue
		}
		_ = p.ui.SetItem(slot, stack)
	}
	return p.ui.Revision(), nil
}
