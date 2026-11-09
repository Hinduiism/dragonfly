package session

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func waitSkinRefresh(t *testing.T, target *Session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for target.skinRefreshPending.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if target.skinRefreshPending.Load() {
		t.Fatal("skin refresh did not finish")
	}
}

func TestChangedSkinReassertsCurrentPlayerListForExistingViewers(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 64, nil)
	hidden, _ := newPublicationSession("Hidden", 64, func(_, _ *world.EntityHandle) (bool, bool, uint64) { return false, false, 1 })
	target, conn := newPublicationSession("Alex", 64, nil)
	actor := newPublicationActor(target, "Alex")
	l.Add(target)
	l.Add(viewer)
	l.Add(hidden)
	viewer.viewConnectedPlayer(actor, target)
	for _, s := range []*Session{viewer, hidden, target} {
		drainPublicationPackets(s)
	}
	runtimeID := viewer.ensurePlayerRuntimeID(target)
	value := skin.New(64, 64)
	value.FullID = "updated-random-skin"
	value.Pix[0] = 191
	target.SetPlayerListSkin(value)
	target.PublishPresentationName(actor, "Blue Fox")
	// Discard the synchronous actor/list refresh; a modern client's final
	// skin must also be updated afterwards without requiring another spawn.
	drainPublicationPackets(viewer)
	drainPublicationPackets(target)
	waitSkinRefresh(t, target)
	for _, s := range []*Session{viewer, target} {
		packets := drainPublicationPackets(s)
		if len(packets) != 1 {
			t.Fatalf("delayed packets=%#v, want one player-list ADD", packets)
		}
		list, ok := packets[0].(*packet.PlayerList)
		if !ok || len(list.Entries) != 1 {
			t.Fatalf("delayed packet=%#v", packets[0])
		}
		entry := list.Entries[0]
		if entry.ActionType != protocol.PlayerListActionAdd || entry.UUID != actor.UUID() || entry.Username != "Blue Fox" || entry.XUID != conn.identity.XUID || entry.Skin.FullID != value.FullID || entry.Skin.SkinData[0] != 191 {
			t.Fatalf("incorrect delayed appearance: %#v", entry)
		}
	}
	if viewer.ensurePlayerRuntimeID(target) != runtimeID {
		t.Fatal("skin refresh changed actor identity")
	}
	if packets := drainPublicationPackets(hidden); len(packets) != 0 {
		t.Fatalf("hidden target leaked: %#v", packets)
	}
}

func TestQueuedSkinRefreshUsesLatestSkinAndRespectsNewVisibility(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	var shown atomic.Bool
	shown.Store(true)
	var revision atomic.Uint64
	viewer, _ := newPublicationSession("Viewer", 64, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return shown.Load(), shown.Load(), revision.Load()
	})
	target, _ := newPublicationSession("Alex", 64, nil)
	l.Add(target)
	l.Add(viewer)
	drainPublicationPackets(viewer)
	drainPublicationPackets(target)
	for _, id := range []string{"old-skin", "latest-skin"} {
		value := skin.New(64, 64)
		value.FullID = id
		target.SetPlayerListSkin(value)
	}
	shown.Store(false)
	revision.Store(1)
	waitSkinRefresh(t, target)
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("refresh ignored latest visibility: %#v", packets)
	}
	packets := drainPublicationPackets(target)
	if len(packets) != 1 {
		t.Fatalf("coalesced updates=%d, want one", len(packets))
	}
	if entry := packets[0].(*packet.PlayerList).Entries[0]; entry.Skin.FullID != "latest-skin" {
		t.Fatalf("stale skin=%q", entry.Skin.FullID)
	}
}

func TestQueuedSkinRefreshDoesNotPublishDisconnectedTarget(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 64, nil)
	target, _ := newPublicationSession("Alex", 64, nil)
	l.Add(target)
	l.Add(viewer)
	value := skin.New(64, 64)
	value.FullID = "departed-skin"
	target.SetPlayerListSkin(value)
	l.Remove(target, nil)
	drainPublicationPackets(viewer)
	drainPublicationPackets(target)
	waitSkinRefresh(t, target)
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("disconnected target reappeared: %#v", packets)
	}
}
