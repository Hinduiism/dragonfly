package world

// ChunkLease keeps one loaded chunk available without registering a Viewer or
// Loader. It is useful for short-lived background work that must hand a loaded
// chunk back to a world transaction without enabling chunk simulation.
//
// A ChunkLease must be released from a transaction belonging to the World that
// created it. ChunkLease is not safe for concurrent use.
type ChunkLease struct {
	w        *World
	pos      ChunkPos
	released bool
}

// Position returns the position of the chunk held by the lease.
func (lease *ChunkLease) Position() ChunkPos {
	if lease == nil {
		return ChunkPos{}
	}
	return lease.pos
}

// Release relinquishes the lease. Release returns true if the chunk was held
// and this call released it. It returns false for a nil or already released
// lease, or when tx belongs to a different World.
func (lease *ChunkLease) Release(tx *Tx) bool {
	if lease == nil || tx == nil || lease.released || lease.w != tx.World() {
		return false
	}
	lease.released = true

	column, ok := lease.w.loadedChunk(lease.pos)
	if !ok || column.leases == 0 {
		return false
	}
	column.leases--
	return true
}

// AcquireChunkLease loads the chunk at pos asynchronously and calls callback
// from a World transaction once it is available. callback receives nil when
// the chunk could not be loaded. AcquireChunkLease returns false when the
// request could not be scheduled, in which case callback is not called.
//
// Already loaded chunks call callback immediately. The returned lease does not
// register a Viewer or Loader and must eventually be released.
func (tx *Tx) AcquireChunkLease(pos ChunkPos, callback func(*Tx, *ChunkLease)) bool {
	if tx == nil || callback == nil {
		return false
	}
	w := tx.World()
	return w.loadChunkAsync(tx, pos, func(callbackTx *Tx, column *Column) {
		loaded, ok := w.loadedChunk(pos)
		if column == nil || !ok || loaded != column {
			callback(callbackTx, nil)
			return
		}
		column.leases++
		callback(callbackTx, &ChunkLease{w: w, pos: pos})
	})
}
