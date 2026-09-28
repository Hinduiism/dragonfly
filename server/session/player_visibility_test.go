package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestClearHiddenEntity(t *testing.T) {
	handle := world.EntitySpawnOpts{}.New(visibilityEntityType{}, visibilityEntityConfig{})
	entity := visibilityEntity{handle: handle}
	s := &Session{hiddenEntities: map[uuid.UUID]struct{}{handle.UUID(): {}}}

	s.ClearHiddenEntity(entity)
	if _, ok := s.hiddenEntities[handle.UUID()]; ok {
		t.Fatal("ClearHiddenEntity() retained hidden marker")
	}
}

func TestConnectedPlayerVisibilitySuppressesAndReplaysActor(t *testing.T) {
	var entityVisible, listed atomic.Bool
	var revision atomic.Uint64
	revision.Store(1)
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return entityVisible.Load(), listed.Load(), revision.Load()
	})
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)

	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial hidden publication failed")
	}
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("hidden actor emitted packets: %#v", packets)
	}
	viewer.entityMutex.RLock()
	runtimeID := viewer.entityRuntimeIDs[target.ent]
	viewer.entityMutex.RUnlock()
	if runtimeID <= selfEntityRuntimeID {
		t.Fatalf("hidden actor runtime ID = %d", runtimeID)
	}

	updatedSkin := skin.New(64, 64)
	updatedSkin.FullID = "updated-hidden-skin"
	target.SetPlayerListSkin(updatedSkin)
	entityVisible.Store(true)
	listed.Store(true)
	revision.Store(2)
	if !viewer.refreshConnectedPlayer(actor, target) {
		t.Fatal("actor reveal failed")
	}
	packets := drainPublicationPackets(viewer)
	if len(packets) != 3 {
		t.Fatalf("reveal emitted %d packets, want 3: %#v", len(packets), packets)
	}
	listAdd, ok := packets[0].(*packet.PlayerList)
	if !ok || len(listAdd.Entries) != 1 || listAdd.Entries[0].ActionType != protocol.PlayerListActionAdd || listAdd.Entries[0].EntityUniqueID != int64(runtimeID) || listAdd.Entries[0].Skin.FullID != "updated-hidden-skin" {
		t.Fatalf("first reveal packet = %#v", packets[0])
	}
	add, ok := packets[1].(*packet.AddPlayer)
	if !ok || add.EntityRuntimeID != runtimeID || add.UUID != actor.UUID() {
		t.Fatalf("second reveal packet = %#v", packets[1])
	}
	skinUpdate, ok := packets[2].(*packet.PlayerSkin)
	if !ok || skinUpdate.UUID != actor.UUID() || skinUpdate.Skin.FullID != "updated-hidden-skin" {
		t.Fatalf("third reveal packet = %#v", packets[2])
	}

	if !viewer.refreshConnectedPlayer(actor, target) {
		t.Fatal("idempotent reveal failed")
	}
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("idempotent reveal emitted packets: %#v", packets)
	}

	entityVisible.Store(false)
	listed.Store(false)
	revision.Store(3)
	if !viewer.refreshConnectedPlayer(actor, target) {
		t.Fatal("actor hide failed")
	}
	packets = drainPublicationPackets(viewer)
	if len(packets) != 2 {
		t.Fatalf("hide emitted %d packets, want 2: %#v", len(packets), packets)
	}
	if remove, ok := packets[0].(*packet.RemoveActor); !ok || remove.EntityUniqueID != int64(runtimeID) {
		t.Fatalf("first hide packet = %#v", packets[0])
	}
	if remove, ok := packets[1].(*packet.PlayerList); !ok || len(remove.Entries) != 1 || remove.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("second hide packet = %#v", packets[1])
	}
	if ok, _ := viewer.reconcileConnectedPlayer(actor, target, viewer.snapshotPlayerActor(actor, target), true, true, 2, false); !ok {
		t.Fatal("stale reveal should be treated as superseded")
	}
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("stale reveal emitted packets: %#v", packets)
	}
}

func TestConnectedPlayerExplicitHideAndSameRevisionReplay(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return true, true, 4
	})
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial actor publication failed")
	}
	_ = drainPublicationPackets(viewer)

	viewer.StopShowingEntity(actor)
	packets := drainPublicationPackets(viewer)
	if len(packets) != 1 {
		t.Fatalf("explicit hide emitted %d packets, want 1: %#v", len(packets), packets)
	}
	if _, ok := packets[0].(*packet.RemoveActor); !ok {
		t.Fatalf("explicit hide packet = %#v", packets[0])
	}

	viewer.StartShowingEntity(actor)
	packets = drainPublicationPackets(viewer)
	addCount := 0
	for _, pk := range packets {
		if _, ok := pk.(*packet.AddPlayer); ok {
			addCount++
		}
	}
	if addCount != 1 {
		t.Fatalf("same-revision replay emitted %d AddPlayer packets, want 1: %#v", addCount, packets)
	}
	viewer.StartShowingEntity(actor)
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("idempotent StartShowingEntity emitted packets: %#v", packets)
	}
}

func TestConnectedPlayerWorldHideRetiresStreamedActor(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return true, true, 1
	})
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial actor publication failed")
	}
	_ = drainPublicationPackets(viewer)

	viewer.HideEntity(actor)
	_ = drainPublicationPackets(viewer)
	viewer.StartShowingEntity(actor)
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("out-of-range actor replayed: %#v", packets)
	}
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("world restream failed")
	}
	packets := drainPublicationPackets(viewer)
	if len(packets) == 0 {
		t.Fatal("world restream did not replay actor")
	}
	if _, ok := packets[0].(*packet.AddPlayer); !ok {
		t.Fatalf("world restream first packet = %#v", packets[0])
	}
}

func TestConnectedPlayerMayRemainVisibleWhileUnlisted(t *testing.T) {
	var entityVisible atomic.Bool
	var listed atomic.Bool
	var revision atomic.Uint64
	entityVisible.Store(true)
	revision.Store(1)
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return entityVisible.Load(), listed.Load(), revision.Load()
	})
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("unlisted actor publication failed")
	}
	packets := drainPublicationPackets(viewer)
	if len(packets) != 4 {
		t.Fatalf("unlisted actor emitted %d packets, want 4: %#v", len(packets), packets)
	}
	if list, ok := packets[0].(*packet.PlayerList); !ok || list.Entries[0].ActionType != protocol.PlayerListActionAdd {
		t.Fatalf("temporary list add = %#v", packets[0])
	}
	if _, ok := packets[1].(*packet.AddPlayer); !ok {
		t.Fatalf("actor add = %#v", packets[1])
	}
	if _, ok := packets[2].(*packet.PlayerSkin); !ok {
		t.Fatalf("actor skin = %#v", packets[2])
	}
	if list, ok := packets[3].(*packet.PlayerList); !ok || list.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("temporary list remove = %#v", packets[3])
	}

	entityVisible.Store(false)
	listed.Store(true)
	revision.Store(2)
	if !viewer.refreshConnectedPlayer(actor, target) {
		t.Fatal("listed-only transition failed")
	}
	packets = drainPublicationPackets(viewer)
	if len(packets) != 2 {
		t.Fatalf("listed-only transition emitted %d packets, want 2: %#v", len(packets), packets)
	}
	if _, ok := packets[0].(*packet.RemoveActor); !ok {
		t.Fatalf("listed-only actor remove = %#v", packets[0])
	}
	if list, ok := packets[1].(*packet.PlayerList); !ok || list.Entries[0].ActionType != protocol.PlayerListActionAdd {
		t.Fatalf("listed-only list add = %#v", packets[1])
	}
}

func TestConnectedPlayerDisconnectRemovesActorBeforeListEntry(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, nil)
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial actor publication failed")
	}
	_ = drainPublicationPackets(viewer)

	l.Remove(target, nil)
	packets := drainPublicationPackets(viewer)
	if len(packets) != 2 {
		t.Fatalf("disconnect emitted %d packets, want 2: %#v", len(packets), packets)
	}
	if _, ok := packets[0].(*packet.RemoveActor); !ok {
		t.Fatalf("disconnect actor remove = %#v", packets[0])
	}
	if list, ok := packets[1].(*packet.PlayerList); !ok || list.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("disconnect list remove = %#v", packets[1])
	}
	viewer.publicationMu.RLock()
	_, retainedList := viewer.playerList[target.ent]
	_, retainedActor := viewer.playerActors[target.ent]
	viewer.publicationMu.RUnlock()
	if retainedList || retainedActor {
		t.Fatal("disconnect retained publication state")
	}
}

func TestSuppressedConnectedPlayerDropsIdentifyingUpdates(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 32, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return false, false, 1
	})
	target, _ := newPublicationSession("Target", 32, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial hidden publication failed")
	}

	viewer.ViewEntityGameMode(actor)
	viewer.ViewEntityMovement(actor, mgl64.Vec3{}, cube.Rotation{}, true)
	viewer.ViewEntityVelocity(actor, mgl64.Vec3{1})
	viewer.ViewEntityTeleport(actor, mgl64.Vec3{1})
	viewer.ViewEntityAction(actor, entity.HurtAction{})
	viewer.ViewEntityState(actor)
	viewer.ViewEntityAnimation(actor, world.EntityAnimation{})
	viewer.ViewEmote(actor, uuid.New())
	viewer.ViewSkin(actor)
	viewer.ViewEntityWake(actor)
	if packets := drainPublicationPackets(viewer); len(packets) != 0 {
		t.Fatalf("suppressed actor emitted identifying updates: %#v", packets)
	}
}

func TestConnectedPlayerRevealSaturationClosesViewer(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, conn := newPublicationSession("Viewer", 1, func(_, _ *world.EntityHandle) (bool, bool, uint64) {
		return true, true, 2
	})
	target, _ := newPublicationSession("Target", 1, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	viewer.playerList[target.ent] = playerListState{listed: true, initialized: true, revision: 1}
	viewer.playerActors[target.ent] = playerActorState{streamed: true, allowed: false, initialized: true, revision: 1}
	viewer.packets <- &packet.PlayStatus{}
	if viewer.refreshConnectedPlayer(actor, target) {
		t.Fatal("saturated actor publication reported success")
	}
	if !conn.closed.Load() {
		t.Fatal("saturated viewer connection remained open")
	}
}

func TestConnectedPlayerConcurrentRefreshAndRemove(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	viewer, _ := newPublicationSession("Viewer", 8192, nil)
	target, _ := newPublicationSession("Target", 8192, nil)
	actor := newPublicationActor(target, "Target")
	setPublicationSessions(l, viewer, target)
	if !viewer.viewConnectedPlayer(actor, target) {
		t.Fatal("initial actor publication failed")
	}
	_ = drainPublicationPackets(viewer)

	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 32 {
				viewer.refreshConnectedPlayer(actor, target)
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
		t.Fatal("actor refresh and removal deadlocked")
	}
	viewer.publicationMu.RLock()
	_, retainedList := viewer.playerList[target.ent]
	_, retainedActor := viewer.playerActors[target.ent]
	viewer.publicationMu.RUnlock()
	if retainedList || retainedActor {
		t.Fatal("removed target retained actor publication state")
	}
}

func withPublicationSessionList(t *testing.T, current *sessionList) {
	t.Helper()
	previous := sessions
	sessions = current
	t.Cleanup(func() { sessions = previous })
}

func drainPublicationPackets(s *Session) []packet.Packet {
	var packets []packet.Packet
	for {
		select {
		case pk := <-s.packets:
			packets = append(packets, pk)
		default:
			return packets
		}
	}
}

type publicationActor struct {
	visibilityEntity
	name       string
	id         uuid.UUID
	appearance skin.Skin
}

func newPublicationActor(target *Session, name string) publicationActor {
	return publicationActor{
		visibilityEntity: visibilityEntity{handle: target.ent},
		name:             name,
		id:               target.ent.UUID(),
		appearance:       skin.New(64, 64),
	}
}

func (a publicationActor) Name() string           { return a.name }
func (a publicationActor) UUID() uuid.UUID        { return a.id }
func (publicationActor) GameMode() world.GameMode { return world.GameModeSurvival }
func (a publicationActor) Skin() skin.Skin        { return a.appearance }

type visibilityEntity struct{ handle *world.EntityHandle }

func (e visibilityEntity) H() *world.EntityHandle { return e.handle }
func (visibilityEntity) Position() mgl64.Vec3     { return mgl64.Vec3{} }
func (visibilityEntity) Rotation() cube.Rotation  { return cube.Rotation{} }
func (visibilityEntity) Close() error             { return nil }

type visibilityEntityConfig struct{}

func (visibilityEntityConfig) Apply(*world.EntityData) {}

type visibilityEntityType struct{}

func (visibilityEntityType) Open(_ *world.Tx, handle *world.EntityHandle, _ *world.EntityData) world.Entity {
	return visibilityEntity{handle: handle}
}
func (visibilityEntityType) EncodeEntity() string { return "test:visibility" }
func (visibilityEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(0, 0, 0, 1, 1, 1)
}
func (visibilityEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (visibilityEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
