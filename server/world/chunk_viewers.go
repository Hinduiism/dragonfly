package world

type chunkViewerWatch struct {
	callback func(*Tx, bool)
	viewed   bool
	pending  bool
}

// ChunkHasViewers reports whether a real viewer has received this chunk.
// It does not load the chunk. Loaders with a NopViewer do not count.
func (tx *Tx) ChunkHasViewers(pos ChunkPos) bool {
	if column, ok := tx.World().loadedChunk(pos); ok {
		for _, viewer := range column.viewers {
			switch viewer.(type) {
			case nil, NopViewer, *NopViewer:
			default:
				return true
			}
		}
	}
	return false
}

// WatchChunkViewers observes changes between zero and at least one viewer.
// The initial state is available from ChunkHasViewers. Changes in one
// transaction are coalesced and delivered after loader locks are released.
// Call the returned function with a transaction in this world to unsubscribe.
func (tx *Tx) WatchChunkViewers(pos ChunkPos, callback func(*Tx, bool)) func(*Tx) {
	if callback == nil {
		return func(*Tx) {}
	}
	w := tx.World()
	if w.chunkViewerWatches == nil {
		w.chunkViewerWatches = make(map[ChunkPos]map[*chunkViewerWatch]struct{})
	}
	watches := w.chunkViewerWatches[pos]
	if watches == nil {
		watches = make(map[*chunkViewerWatch]struct{})
		w.chunkViewerWatches[pos] = watches
	}
	watch := &chunkViewerWatch{callback: callback, viewed: tx.ChunkHasViewers(pos)}
	watches[watch] = struct{}{}
	return func(tx *Tx) {
		if tx.World() != w {
			return
		}
		if _, active := watches[watch]; !active {
			return
		}
		delete(watches, watch)
		if len(watches) == 0 {
			delete(w.chunkViewerWatches, pos)
		}
	}
}

func (w *World) notifyChunkViewerWatches(tx *Tx, pos ChunkPos) {
	watches := w.chunkViewerWatches[pos]
	if len(watches) == 0 {
		return
	}
	viewed := tx.ChunkHasViewers(pos)
	for watch := range watches {
		if watch.pending || watch.viewed == viewed {
			continue
		}
		watch.pending = true
		tx.Defer(func(tx *Tx) {
			watch.pending = false
			if _, active := watches[watch]; !active {
				return
			}
			if viewed := tx.ChunkHasViewers(pos); watch.viewed != viewed {
				watch.viewed = viewed
				watch.callback(tx, viewed)
			}
		})
	}
}
