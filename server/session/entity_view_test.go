package session

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestEntityViewLifecycle(t *testing.T) {
	s := newEntityViewTestSession()
	conf := testEntityViewConfig()
	view, err := s.AddEntityView(conf)
	if err != nil {
		t.Fatalf("add entity view: %v", err)
	}

	add := packetOf[*packet.AddActor](t, s)
	if add.EntityRuntimeID != view.id || add.EntityUniqueID != int64(view.id) {
		t.Fatalf("spawn IDs: got runtime=%v unique=%v, want %v", add.EntityRuntimeID, add.EntityUniqueID, view.id)
	}
	if add.EntityType != conf.Identifier || add.Position != vec64To32(conf.Position) || add.Velocity != vec64To32(conf.Velocity) {
		t.Fatalf("unexpected spawn packet: %#v", add)
	}
	if got := add.EntityMetadata[protocol.EntityDataKeyWidth]; got != float32(conf.Bounds.Width()) {
		t.Fatalf("metadata width: got %v, want %v", got, conf.Bounds.Width())
	}
	if got := add.EntityMetadata[protocol.EntityDataKeyHeight]; got != float32(conf.Bounds.Height()) {
		t.Fatalf("metadata height: got %v, want %v", got, conf.Bounds.Height())
	}
	if got := add.EntityMetadata[protocol.EntityDataKeyScale]; got != float32(1) {
		t.Fatalf("metadata scale: got %v, want 1", got)
	}
	if !add.EntityMetadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI) {
		t.Fatal("metadata does not mark the view as immobile")
	}
	if got := add.EntityMetadata[protocol.EntityDataKeyName]; got != conf.NameTag {
		t.Fatalf("metadata name tag: got %q, want %q", got, conf.NameTag)
	}

	if err := view.Move(conf.Position, conf.Rotation, false); err != nil {
		t.Fatalf("repeat initial transform: %v", err)
	}
	assertNoEntityViewPacket(t, s)

	movePos := mgl64.Vec3{4, 5, 6}
	moveRot := cube.Rotation{35, -10}
	if err := view.Move(movePos, moveRot, true); err != nil {
		t.Fatalf("move entity view: %v", err)
	}
	move := packetOf[*packet.MoveActorAbsolute](t, s)
	if move.EntityRuntimeID != view.id || move.Position != vec64To32(movePos) {
		t.Fatalf("unexpected movement packet: %#v", move)
	}
	if move.Flags != packet.MoveFlagOnGround {
		t.Fatalf("move flags: got %v, want on-ground", move.Flags)
	}

	teleportPos := mgl64.Vec3{7, 8, 9}
	if err := view.Teleport(teleportPos, moveRot, false); err != nil {
		t.Fatalf("teleport entity view: %v", err)
	}
	teleport := packetOf[*packet.MoveActorAbsolute](t, s)
	if teleport.Flags != packet.MoveFlagTeleport || teleport.Position != vec64To32(teleportPos) {
		t.Fatalf("unexpected teleport packet: %#v", teleport)
	}

	if err := view.Close(); err != nil {
		t.Fatalf("close entity view: %v", err)
	}
	remove := packetOf[*packet.RemoveActor](t, s)
	if remove.EntityUniqueID != int64(view.id) {
		t.Fatalf("remove ID: got %v, want %v", remove.EntityUniqueID, view.id)
	}
	if err := view.Close(); err != nil {
		t.Fatalf("close entity view again: %v", err)
	}
	assertNoEntityViewPacket(t, s)
	if err := view.Move(movePos, moveRot, false); !errors.Is(err, ErrEntityViewClosed) {
		t.Fatalf("move closed view: got %v, want ErrEntityViewClosed", err)
	}
}

func TestEntityViewInvalidConfigurationDoesNotAllocate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EntityViewConfig)
	}{
		{name: "empty identifier", mutate: func(c *EntityViewConfig) { c.Identifier = "" }},
		{name: "position", mutate: func(c *EntityViewConfig) { c.Position[0] = math.NaN() }},
		{name: "rotation", mutate: func(c *EntityViewConfig) { c.Rotation[0] = math.Inf(1) }},
		{name: "velocity", mutate: func(c *EntityViewConfig) { c.Velocity[2] = math.Inf(-1) }},
		{name: "bounds", mutate: func(c *EntityViewConfig) { c.Bounds = cube.Box(math.NaN(), 0, 0, 1, 1, 1) }},
		{name: "scale", mutate: func(c *EntityViewConfig) { c.Scale = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newEntityViewTestSession()
			conf := testEntityViewConfig()
			test.mutate(&conf)
			before := s.currentEntityRuntimeID
			if _, err := s.AddEntityView(conf); !errors.Is(err, ErrEntityViewInvalid) {
				t.Fatalf("add invalid entity view: got %v, want ErrEntityViewInvalid", err)
			}
			if s.currentEntityRuntimeID != before {
				t.Fatalf("runtime ID changed from %v to %v", before, s.currentEntityRuntimeID)
			}
			assertNoEntityViewPacket(t, s)
		})
	}
}

func TestEntityViewRuntimeIDIsolation(t *testing.T) {
	s := newEntityViewTestSession()
	normal := &world.EntityHandle{}
	s.entityMutex.Lock()
	s.currentEntityRuntimeID++
	normalID := s.currentEntityRuntimeID
	s.entityRuntimeIDs[normal] = normalID
	s.entities[normalID] = normal
	s.entityMutex.Unlock()

	view, err := s.AddEntityView(testEntityViewConfig())
	if err != nil {
		t.Fatalf("add entity view: %v", err)
	}
	_ = packetOf[*packet.AddActor](t, s)
	if view.id == normalID {
		t.Fatalf("viewer-local runtime ID %v collided with normal entity", view.id)
	}
	if _, ok := s.entities[view.id]; ok {
		t.Fatal("viewer-local runtime ID was added to authoritative entity lookup")
	}
	for _, id := range s.entityRuntimeIDs {
		if id == view.id {
			t.Fatal("viewer-local runtime ID was added to normal entity mappings")
		}
	}
}

func TestClearEntityViewsInvalidatesHandles(t *testing.T) {
	s := newEntityViewTestSession()
	first, err := s.AddEntityView(testEntityViewConfig())
	if err != nil {
		t.Fatalf("add first entity view: %v", err)
	}
	second, err := s.AddEntityView(testEntityViewConfig())
	if err != nil {
		t.Fatalf("add second entity view: %v", err)
	}
	_ = packetOf[*packet.AddActor](t, s)
	_ = packetOf[*packet.AddActor](t, s)

	s.clearEntityViews()
	removed := map[int64]bool{}
	removed[packetOf[*packet.RemoveActor](t, s).EntityUniqueID] = true
	removed[packetOf[*packet.RemoveActor](t, s).EntityUniqueID] = true
	if !removed[int64(first.id)] || !removed[int64(second.id)] {
		t.Fatalf("cleanup removed IDs %v, want %v and %v", removed, first.id, second.id)
	}
	if len(s.entityViews) != 0 {
		t.Fatalf("active views after cleanup: %v", len(s.entityViews))
	}
	if err := first.Move(mgl64.Vec3{1, 2, 3}, cube.Rotation{}, false); !errors.Is(err, ErrEntityViewClosed) {
		t.Fatalf("move invalidated view: got %v, want ErrEntityViewClosed", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close invalidated view: %v", err)
	}
	assertNoEntityViewPacket(t, s)
}

func TestEntityViewInteractionIsIgnored(t *testing.T) {
	s := newEntityViewTestSession()
	view, err := s.AddEntityView(testEntityViewConfig())
	if err != nil {
		t.Fatalf("add entity view: %v", err)
	}
	_ = packetOf[*packet.AddActor](t, s)

	h := &InventoryTransactionHandler{}
	err = h.handleUseItemOnEntityTransaction(&protocol.UseItemOnEntityTransactionData{
		TargetEntityRuntimeID: view.id,
		ActionType:            protocol.UseItemOnEntityActionAttack,
	}, s, nil, nil)
	if err != nil {
		t.Fatalf("interact with entity view: %v", err)
	}
}

func TestEntityViewConcurrentMoveAndCloseOrdering(t *testing.T) {
	s := newEntityViewTestSession()
	view, err := s.AddEntityView(testEntityViewConfig())
	if err != nil {
		t.Fatalf("add entity view: %v", err)
	}
	_ = packetOf[*packet.AddActor](t, s)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 1; i <= 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_ = view.Move(mgl64.Vec3{float64(i), 2, 3}, cube.Rotation{float64(i), 0}, false)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_ = view.Close()
	}()
	close(start)
	wg.Wait()

	removed := false
	removeCount := 0
	for {
		select {
		case pk := <-s.packets:
			switch pk.(type) {
			case *packet.MoveActorAbsolute:
				if removed {
					t.Fatal("movement packet was queued after RemoveActor")
				}
			case *packet.RemoveActor:
				removed = true
				removeCount++
			}
		default:
			if !removed || removeCount != 1 {
				t.Fatalf("remove packets: got %v, want exactly one", removeCount)
			}
			return
		}
	}
}

func TestEntityViewClosedSessionDoesNotAllocate(t *testing.T) {
	s := newEntityViewTestSession()
	close(s.closeBackground)
	before := s.currentEntityRuntimeID
	if _, err := s.AddEntityView(testEntityViewConfig()); !errors.Is(err, ErrEntityViewUnavailable) {
		t.Fatalf("add entity view to closed session: got %v, want ErrEntityViewUnavailable", err)
	}
	if s.currentEntityRuntimeID != before {
		t.Fatalf("runtime ID changed from %v to %v", before, s.currentEntityRuntimeID)
	}
}

func newEntityViewTestSession() *Session {
	return &Session{
		conf:                   Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		packets:                make(chan packet.Packet, 1024),
		closeBackground:        make(chan struct{}),
		currentEntityRuntimeID: selfEntityRuntimeID,
		entityRuntimeIDs:       map[*world.EntityHandle]uint64{},
		entities:               map[uint64]*world.EntityHandle{},
		hiddenEntities:         map[uuid.UUID]struct{}{},
		entityViews:            map[uint64]entityViewState{},
	}
}

func testEntityViewConfig() EntityViewConfig {
	return EntityViewConfig{
		Identifier:        "dragonfly:test_view",
		Position:          mgl64.Vec3{1, 2, 3},
		Rotation:          cube.Rotation{10, 20},
		Velocity:          mgl64.Vec3{0.1, 0.2, 0.3},
		Bounds:            cube.Box(-0.5, 0, -0.5, 0.5, 2, 0.5),
		Immobile:          true,
		NameTag:           "Test View",
		AlwaysShowNameTag: true,
	}
}

func packetOf[T packet.Packet](t *testing.T, s *Session) T {
	t.Helper()
	select {
	case pk := <-s.packets:
		got, ok := pk.(T)
		if !ok {
			var zero T
			t.Fatalf("packet type: got %T, want %T", pk, zero)
		}
		return got
	default:
		var zero T
		t.Fatalf("no packet queued, want %T", zero)
		return zero
	}
}

func assertNoEntityViewPacket(t *testing.T, s *Session) {
	t.Helper()
	select {
	case pk := <-s.packets:
		t.Fatalf("unexpected packet %T", pk)
	default:
	}
}
