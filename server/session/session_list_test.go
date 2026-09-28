package session

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type publicationConn struct {
	identity login.IdentityData
	closed   atomic.Bool
}

func (c *publicationConn) Close() error                     { c.closed.Store(true); return nil }
func (c *publicationConn) IdentityData() login.IdentityData { return c.identity }
func (*publicationConn) ClientData() login.ClientData       { return login.ClientData{} }
func (*publicationConn) ClientCacheEnabled() bool           { return false }
func (*publicationConn) ChunkRadius() int                   { return 8 }
func (*publicationConn) Latency() time.Duration             { return 0 }
func (*publicationConn) Flush() error                       { return nil }
func (*publicationConn) RemoteAddr() net.Addr               { return &net.UDPAddr{} }
func (*publicationConn) ReadPacket() (packet.Packet, error) { return nil, io.EOF }
func (*publicationConn) WritePacket(packet.Packet) error    { return nil }
func (*publicationConn) StartGameContext(context.Context, minecraft.GameData) error {
	return nil
}

func newPublicationSession(name string, capacity int, policy func(viewer, target *world.EntityHandle) (bool, bool, uint64)) (*Session, *publicationConn) {
	id := uuid.New()
	handle := world.EntitySpawnOpts{ID: id}.New(visibilityEntityType{}, visibilityEntityConfig{})
	conn := &publicationConn{identity: login.IdentityData{Identity: id.String(), DisplayName: name, XUID: id.String()}}
	s := &Session{
		conf:                   Config{PlayerVisibility: policy},
		ent:                    handle,
		conn:                   conn,
		packets:                make(chan packet.Packet, capacity),
		closeBackground:        make(chan struct{}),
		entityRuntimeIDs:       make(map[*world.EntityHandle]uint64),
		entities:               make(map[uint64]*world.EntityHandle),
		hiddenEntities:         make(map[uuid.UUID]struct{}),
		playerList:             make(map[*world.EntityHandle]playerListState),
		currentEntityRuntimeID: selfEntityRuntimeID,
	}
	s.SetPlayerListSkin(skin.New(64, 64))
	return s, conn
}

func playerListPackets(s *Session) []*packet.PlayerList {
	var packets []*packet.PlayerList
	for {
		select {
		case pk := <-s.packets:
			if list, ok := pk.(*packet.PlayerList); ok {
				packets = append(packets, list)
			}
		default:
			return packets
		}
	}
}

func TestSessionListNilPolicyPreservesAddsAndRemoves(t *testing.T) {
	l := new(sessionList)
	viewer, _ := newPublicationSession("Viewer", 16, nil)
	target, _ := newPublicationSession("Target", 16, nil)
	l.Add(viewer)
	_ = playerListPackets(viewer)
	l.Add(target)

	adds := playerListPackets(viewer)
	if len(adds) != 1 || len(adds[0].Entries) != 1 || adds[0].Entries[0].ActionType != protocol.PlayerListActionAdd || adds[0].Entries[0].UUID != target.ent.UUID() {
		t.Fatalf("viewer additions = %#v", adds)
	}
	viewer.entityMutex.RLock()
	runtimeID := viewer.entityRuntimeIDs[target.ent]
	viewer.entityMutex.RUnlock()
	if runtimeID <= selfEntityRuntimeID {
		t.Fatalf("target runtime ID = %d", runtimeID)
	}

	l.Remove(target, nil)
	removes := playerListPackets(viewer)
	if len(removes) != 1 || removes[0].Entries[0].ActionType != protocol.PlayerListActionRemove || removes[0].Entries[0].UUID != target.ent.UUID() {
		t.Fatalf("viewer removals = %#v", removes)
	}
	viewer.entityMutex.RLock()
	_, retained := viewer.entityRuntimeIDs[target.ent]
	viewer.entityMutex.RUnlock()
	if retained {
		t.Fatal("departing target retained a runtime ID")
	}
}

func TestSessionListPolicyReconcilesWithoutChangingRuntimeID(t *testing.T) {
	var listed atomic.Bool
	var revision atomic.Uint64
	revision.Store(1)
	var viewerHandle, targetHandle *world.EntityHandle
	policy := func(viewer, target *world.EntityHandle) (bool, bool, uint64) {
		if viewer == viewerHandle && target == targetHandle {
			return listed.Load(), listed.Load(), revision.Load()
		}
		return true, true, revision.Load()
	}
	l := new(sessionList)
	viewer, _ := newPublicationSession("Viewer", 16, policy)
	target, _ := newPublicationSession("Target", 16, policy)
	viewerHandle, targetHandle = viewer.ent, target.ent
	l.Add(viewer)
	_ = playerListPackets(viewer)
	l.Add(target)
	if got := playerListPackets(viewer); len(got) != 0 {
		t.Fatalf("hidden target was listed: %#v", got)
	}
	viewer.entityMutex.RLock()
	runtimeID := viewer.entityRuntimeIDs[target.ent]
	viewer.entityMutex.RUnlock()
	if runtimeID <= selfEntityRuntimeID {
		t.Fatalf("hidden target runtime ID = %d", runtimeID)
	}

	updatedSkin := skin.New(64, 64)
	updatedSkin.FullID = "updated-skin"
	target.SetPlayerListSkin(updatedSkin)
	listed.Store(true)
	revision.Store(2)
	if !l.reconcilePlayerList(target, viewer) {
		t.Fatal("reveal reconciliation failed")
	}
	adds := playerListPackets(viewer)
	if len(adds) != 1 || adds[0].Entries[0].EntityUniqueID != int64(runtimeID) || adds[0].Entries[0].Skin.FullID != "updated-skin" {
		t.Fatalf("reveal packet = %#v", adds)
	}

	listed.Store(false)
	revision.Store(3)
	if !l.reconcilePlayerList(target, viewer) {
		t.Fatal("hide reconciliation failed")
	}
	removes := playerListPackets(viewer)
	if len(removes) != 1 || removes[0].Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("hide packet = %#v", removes)
	}
	if !l.applyPlayerListDecision(target, viewer, true, 2) {
		t.Fatal("stale decision should be treated as already superseded")
	}
	if got := playerListPackets(viewer); len(got) != 0 {
		t.Fatalf("stale reveal emitted packets: %#v", got)
	}
	viewer.entityMutex.RLock()
	retained := viewer.entityRuntimeIDs[target.ent]
	viewer.entityMutex.RUnlock()
	if retained != runtimeID {
		t.Fatalf("runtime ID changed: got %d, want %d", retained, runtimeID)
	}
}

func TestSessionListPublicationSaturationClosesViewer(t *testing.T) {
	l := new(sessionList)
	viewer, conn := newPublicationSession("Viewer", 1, nil)
	target, _ := newPublicationSession("Target", 1, nil)
	l.s = []*Session{viewer, target}
	viewer.packets <- &packet.PlayStatus{}
	if l.reconcilePlayerList(target, viewer) {
		t.Fatal("saturated publication reported success")
	}
	if !conn.closed.Load() {
		t.Fatal("saturated viewer connection remained open")
	}
}

func TestSessionListConcurrentRefreshAndRemove(t *testing.T) {
	l := new(sessionList)
	viewer, _ := newPublicationSession("Viewer", 256, nil)
	target, _ := newPublicationSession("Target", 256, nil)
	l.Add(viewer)
	_ = playerListPackets(viewer)
	l.Add(target)
	_ = playerListPackets(viewer)

	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 32 {
				l.reconcilePlayerList(target, viewer)
			}
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		l.Remove(target, nil)
	}()
	close(start)
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh and remove deadlocked")
	}
	viewer.publicationMu.Lock()
	_, retained := viewer.playerList[target.ent]
	viewer.publicationMu.Unlock()
	if retained {
		t.Fatal("removed target retained player-list state")
	}
}
