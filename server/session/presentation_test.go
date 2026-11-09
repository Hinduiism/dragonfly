package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func BenchmarkPresentationRefresh(b *testing.B) {
	for _, count := range []int{1, 32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			previous := sessions
			l := new(sessionList)
			sessions = l
			defer func() { sessions = previous }()
			target, _ := newPublicationSession("Alex", 128, nil)
			actor := newPublicationActor(target, "Alex")
			viewers := make([]*Session, 0, count+1)
			for range count {
				viewer, _ := newPublicationSession("Viewer", 128, nil)
				viewers = append(viewers, viewer)
			}
			setPublicationSessions(l, append(viewers, target)...)
			for _, viewer := range viewers {
				viewer.viewConnectedPlayer(actor, target)
				drainPublicationPackets(viewer)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				name := "Shadow"
				if i%2 == 0 {
					name = "Blue Fox"
				}
				target.PublishPresentationName(actor, name)
				for _, viewer := range viewers {
					drainPublicationPackets(viewer)
				}
				drainPublicationPackets(target)
			}
		})
	}
}

func TestPresentationNameRefreshPreservesIdentityAndVisibility(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 128, nil)
	hidden, _ := newPublicationSession("HiddenViewer", 128, func(_, _ *world.EntityHandle) (bool, bool, uint64) { return false, false, 7 })
	target, conn := newPublicationSession("Alex", 128, nil)
	actor := newPublicationActor(target, "Alex")
	setPublicationSessions(l, viewer, hidden, target)
	if !viewer.viewConnectedPlayer(actor, target) || !hidden.viewConnectedPlayer(actor, target) {
		t.Fatal("initial publication")
	}
	drainPublicationPackets(viewer)
	drainPublicationPackets(hidden)
	id := viewer.entityRuntimeIDs[target.ent]
	target.PublishPresentationName(actor, "Blue Fox")
	pkts := drainPublicationPackets(viewer)
	if len(pkts) != 5 {
		t.Fatalf("refresh packets: %d: %#v", len(pkts), pkts)
	}
	if r, ok := pkts[0].(*packet.PlayerList); !ok || r.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatal("missing old list removal")
	}
	entry := pkts[1].(*packet.PlayerList).Entries[0]
	if entry.Username != "Blue Fox" || entry.XUID != conn.identity.XUID || entry.UUID != actor.UUID() {
		t.Fatalf("wrong identity: %#v", entry)
	}
	add := pkts[3].(*packet.AddPlayer)
	if add.Username != "Blue Fox" || add.EntityRuntimeID != id || add.UUID != actor.UUID() {
		t.Fatalf("wrong actor: %#v", add)
	}
	if len(drainPublicationPackets(hidden)) != 0 {
		t.Fatal("nickname revealed hidden player")
	}
	if conn.identity.DisplayName != "Alex" || actor.Name() != "Alex" {
		t.Fatal("authenticated name changed")
	}
	target.PublishPresentationName(actor, "Blue Fox")
	if len(drainPublicationPackets(viewer)) != 0 {
		t.Fatal("same name republished")
	}
	late, _ := newPublicationSession("Late", 32, nil)
	late.registered.Store(true)
	if !late.viewConnectedPlayer(actor, target) {
		t.Fatal("late viewer")
	}
	if p := drainPublicationPackets(late)[0].(*packet.PlayerList); p.Entries[0].Username != "Blue Fox" {
		t.Fatal("late viewer leaked real name")
	}
	target.PublishPresentationName(actor, "Alex")
	if target.playerListEntry(1).Username != "Alex" {
		t.Fatal("reset")
	}
}

func TestPresentationRefreshPreservesExplicitHideAndRejectsStaleActor(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 128, nil)
	target, _ := newPublicationSession("Alex", 128, nil)
	actor := newPublicationActor(target, "Alex")
	setPublicationSessions(l, viewer, target)
	viewer.viewConnectedPlayer(actor, target)
	drainPublicationPackets(viewer)
	stale := viewer.snapshotPlayerActor(actor, target)
	target.PublishPresentationName(actor, "Shadow")
	drainPublicationPackets(viewer)
	if ok, _ := viewer.applyPlayerActorDecision(actor, target, stale, true, 0, false); !ok {
		t.Fatal("stale decision")
	}
	if len(drainPublicationPackets(viewer)) != 0 {
		t.Fatal("stale actor replayed")
	}
	viewer.StopShowingEntity(actor)
	drainPublicationPackets(viewer)
	target.PublishPresentationName(actor, "Blue Fox")
	for _, p := range drainPublicationPackets(viewer) {
		if _, ok := p.(*packet.AddPlayer); ok {
			t.Fatal("explicitly hidden actor revealed")
		}
	}
}
