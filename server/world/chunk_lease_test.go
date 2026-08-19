package world

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestChunkLeaseRetainsUnusedChunk(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	pos := ChunkPos{4, -7}
	w.Do(func(tx *Tx) {
		var lease *ChunkLease
		if !tx.AcquireChunkLease(pos, func(_ *Tx, acquired *ChunkLease) {
			lease = acquired
		}) {
			t.Fatal("expected chunk lease request to be accepted")
		}
		if lease == nil {
			t.Fatal("expected chunk lease to be acquired")
		}
		if lease.Position() != pos {
			t.Fatalf("lease position = %v, want %v", lease.Position(), pos)
		}

		column, ok := w.loadedChunk(pos)
		if !ok {
			t.Fatal("expected leased chunk to be loaded")
		}
		if len(column.viewers) != 0 || len(column.loaders) != 0 {
			t.Fatal("expected chunk lease not to register a viewer or loader")
		}
		if column.leases != 1 {
			t.Fatalf("column lease count = %d, want 1", column.leases)
		}
		if tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected explicit unload to reject a leased chunk")
		}
		w.closeUnusedChunks(tx)
		if _, ok := w.loadedChunk(pos); !ok {
			t.Fatal("expected periodic sweep to retain a leased chunk")
		}

		if !lease.Release(tx) {
			t.Fatal("expected first release to succeed")
		}
		if lease.Release(tx) {
			t.Fatal("expected repeated release to be idempotent")
		}
		if !tx.UnloadChunkIfUnused(pos) {
			t.Fatal("expected released chunk to be unloadable")
		}
	})
}

func TestChunkLeaseRejectsWrongWorldRelease(t *testing.T) {
	first := Config{Synchronous: true}.New()
	second := Config{Synchronous: true}.New()
	defer first.Close()
	defer second.Close()

	var lease *ChunkLease
	first.Do(func(tx *Tx) {
		if !tx.AcquireChunkLease(ChunkPos{1, 2}, func(_ *Tx, acquired *ChunkLease) {
			lease = acquired
		}) {
			t.Fatal("expected chunk lease request to be accepted")
		}
	})
	if lease == nil {
		t.Fatal("expected chunk lease to be acquired")
	}

	second.Do(func(tx *Tx) {
		if lease.Release(tx) {
			t.Fatal("expected release through another world to fail")
		}
	})
	first.Do(func(tx *Tx) {
		if !lease.Release(tx) {
			t.Fatal("expected lease to remain releasable through its world")
		}
	})
}

func TestChunkLeaseDeduplicatesPendingLoad(t *testing.T) {
	provider := newBlockingLeaseProvider()
	w := Config{Provider: provider}.New()
	defer w.Close()

	pos := ChunkPos{9, 11}
	leases := make(chan *ChunkLease, 2)
	task := w.Do(func(tx *Tx) {
		for range 2 {
			if !tx.AcquireChunkLease(pos, func(_ *Tx, lease *ChunkLease) {
				leases <- lease
			}) {
				t.Error("expected chunk lease request to be accepted")
			}
		}
	})
	waitTask(t, task)
	waitSignal(t, provider.started, "provider load")
	close(provider.release)

	first := receiveLease(t, leases)
	second := receiveLease(t, leases)
	if first == nil || second == nil {
		t.Fatal("expected both pending callers to receive leases")
	}
	if got := provider.loads.Load(); got != 1 {
		t.Fatalf("provider loads = %d, want 1", got)
	}

	waitTask(t, w.Do(func(tx *Tx) {
		if !first.Release(tx) || !second.Release(tx) {
			t.Error("expected both independent leases to release")
		}
	}))
}

func TestChunkLeaseReportsLoadFailure(t *testing.T) {
	w := Config{Provider: failingLeaseProvider{}}.New()
	defer w.Close()

	result := make(chan *ChunkLease, 1)
	waitTask(t, w.Do(func(tx *Tx) {
		if !tx.AcquireChunkLease(ChunkPos{-2, 5}, func(_ *Tx, lease *ChunkLease) {
			result <- lease
		}) {
			t.Error("expected chunk lease request to be accepted")
		}
	}))
	if lease := receiveLease(t, result); lease != nil {
		t.Fatal("expected failed provider load to return a nil lease")
	}
}

func TestChunkLeasePendingRequestCompletesWhenWorldCloses(t *testing.T) {
	provider := newBlockingLeaseProvider()
	w := Config{Provider: provider}.New()

	result := make(chan *ChunkLease, 1)
	waitTask(t, w.Do(func(tx *Tx) {
		if !tx.AcquireChunkLease(ChunkPos{3, 7}, func(_ *Tx, lease *ChunkLease) {
			result <- lease
		}) {
			t.Error("expected chunk lease request to be accepted")
		}
	}))
	waitSignal(t, provider.started, "provider load")

	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	if lease := receiveLease(t, result); lease != nil {
		t.Fatal("expected world close to fail the pending lease request")
	}
	close(provider.release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close world: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("world close remained blocked after the provider request finished")
	}
}

func TestChunkLeaseRejectsNilCallback(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()

	w.Do(func(tx *Tx) {
		if tx.AcquireChunkLease(ChunkPos{}, nil) {
			t.Fatal("expected nil callback to be rejected")
		}
	})
}

type blockingLeaseProvider struct {
	NopProvider
	loads   atomic.Int64
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingLeaseProvider() *blockingLeaseProvider {
	return &blockingLeaseProvider{started: make(chan struct{}), release: make(chan struct{})}
}

func (provider *blockingLeaseProvider) LoadColumn(pos ChunkPos, dimension Dimension) (*chunk.Column, error) {
	provider.loads.Add(1)
	provider.once.Do(func() { close(provider.started) })
	<-provider.release
	return provider.NopProvider.LoadColumn(pos, dimension)
}

type failingLeaseProvider struct{ NopProvider }

func (failingLeaseProvider) LoadColumn(ChunkPos, Dimension) (*chunk.Column, error) {
	return nil, errors.New("test provider failure")
}

func waitTask(t *testing.T, task *Task) {
	t.Helper()
	ctx, cancel := contextWithTestTimeout()
	defer cancel()
	if err := task.Wait(ctx); err != nil {
		t.Fatalf("wait for world task: %v", err)
	}
}

func receiveLease(t *testing.T, leases <-chan *ChunkLease) *ChunkLease {
	t.Helper()
	select {
	case lease := <-leases:
		return lease
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for chunk lease")
		return nil
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func contextWithTestTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
