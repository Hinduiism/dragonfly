package session

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type viewTestType struct {
	schema  world.EntityPropertySchema
	session *Session
}

func (viewTestType) EncodeEntity() string                           { return "test:display" }
func (t viewTestType) EntityProperties() world.EntityPropertySchema { return t.schema }
func (viewTestType) BBox(world.Entity) cube.BBox                    { return cube.BBox{} }
func (viewTestType) EncodeNBT(*world.EntityData) map[string]any     { return nil }
func (viewTestType) DecodeNBT(map[string]any, *world.EntityData)    {}
func (t viewTestType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &viewTestEntity{Ent: entity.Open(tx, h, d), session: t.session}
}

type viewTestEntity struct {
	*entity.Ent
	session *Session
}

func (e *viewTestEntity) BeforeWorldRemoval(*world.Tx) { e.session.ClearEntityViews() }

func newEntityViewFixture(t testing.TB, definitions ...world.EntityPropertyDefinition) (*Session, *world.World) {
	t.Helper()
	s := newEnvironmentViewTestSession()
	s.currentEntityRuntimeID = selfEntityRuntimeID
	s.entities = map[uint64]*world.EntityHandle{}
	s.entityRuntimeIDs = map[*world.EntityHandle]uint64{}
	if len(definitions) == 0 {
		definitions = []world.EntityPropertyDefinition{
			{Name: "test:revision", Type: world.EntityPropertyInt, Min: 0, Max: 100},
			{Name: "test:x", Type: world.EntityPropertyFloat, Min: -100, Max: 100},
		}
	}
	schema, err := world.NewEntityPropertySchema(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	typ := viewTestType{schema: schema, session: s}
	w := world.Config{Synchronous: true, Entities: (world.EntityRegistryConfig{}).New([]world.EntityType{typ})}.New()
	t.Cleanup(func() { _ = w.Close() })
	s.ent = (world.EntitySpawnOpts{}).New(typ, entity.StationaryBehaviourConfig{})
	if err := w.Do(func(tx *world.Tx) { tx.AddEntity(s.ent) }).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, w
}

func testViewState(x float32) EntityViewState {
	return EntityViewState{Position: mgl64.Vec3{0, 64, 0}, Properties: []world.EntityPropertyValue{{Type: world.EntityPropertyInt, Int: 1}, {Type: world.EntityPropertyFloat, Float: x}}}
}

func TestEntityViewSchemaPrivacyCoalescingAndIDs(t *testing.T) {
	s, w := newEntityViewFixture(t)
	other := newEnvironmentViewTestSession()
	var v *EntityView
	if err := w.Do(func(tx *world.Tx) {
		var err error
		s.sendAvailableEntities(w)
		v, err = s.AddEntityView(tx, "test:display", testViewState(0))
		if err != nil {
			t.Fatal(err)
		}
		if v.id <= selfEntityRuntimeID {
			t.Fatal("reserved player ID used")
		}
		packetOf[*packet.AvailableActorIdentifiers](t, s)
		schema := packetOf[*packet.SyncActorProperty](t, s)
		data, err := nbt.MarshalEncoding(schema.PropertyData, nbt.NetworkLittleEndian)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := nbt.UnmarshalEncoding(data, &decoded, nbt.NetworkLittleEndian); err != nil {
			t.Fatal(err)
		}
		if decoded["type"] != "test:display" {
			t.Fatal("schema type missing on wire")
		}
		spawn := packetOf[*packet.AddActor](t, s)
		if spawn.EntityMetadata[protocol.EntityDataKeyWidth] != float32(0) || spawn.EntityMetadata[protocol.EntityDataKeyHeight] != float32(0) {
			t.Fatal("display has a collision box")
		}
		if spawn.EntityProperties.FloatProperties[0].Index != 1 || spawn.EntityProperties.IntegerProperties[0].Index != 0 {
			t.Fatal("property index spaces were split")
		}
		for i := range 20 {
			state := testViewState(float32(i))
			if err := s.UpdateEntityView(tx, v, state); err != nil {
				t.Fatal(err)
			}
			state.Properties[1].Float = -99
		}
		if len(s.packets) != 0 {
			t.Fatal("updates were not coalesced")
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	update := packetOf[*packet.SetActorData](t, s)
	if update.EntityProperties.FloatProperties[0].Value != 19 {
		t.Fatal("snapshot was not copied / latest did not win")
	}
	if len(s.packets) != 0 || len(other.packets) != 0 {
		t.Fatal("extra or public packets")
	}
	if err := w.Do(func(tx *world.Tx) {
		if err := s.UpdateEntityView(tx, v, testViewState(19)); err != nil {
			t.Fatal(err)
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(s.packets) != 0 {
		t.Fatal("unchanged hold emitted packets")
	}
	if err := w.Do(func(tx *world.Tx) {
		s.currentEntityRuntimeID++ // Represents the shared ordinary-entity allocator.
		next, err := s.AddEntityView(tx, "test:display", testViewState(0))
		if err != nil {
			t.Fatal(err)
		}
		if next.id != v.id+2 {
			t.Fatal("private ID bypassed shared allocator")
		}
		if _, ok := (<-s.packets).(*packet.AddActor); !ok {
			t.Fatal("schema resent for the same type")
		}
		if err := s.CloseEntityView(tx, next); err != nil {
			t.Fatal(err)
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestEntityViewInteractionsAreIgnored(t *testing.T) {
	s, w := newEntityViewFixture(t)
	if err := w.Do(func(tx *world.Tx) {
		v, err := s.AddEntityView(tx, "test:display", testViewState(0))
		if err != nil {
			t.Fatal(err)
		}
		for _, closeFirst := range []bool{false, true} {
			if closeFirst {
				if err := s.CloseEntityView(tx, v); err != nil {
					t.Fatal(err)
				}
			}
			for _, action := range []byte{packet.InteractActionMouseOverEntity, 255} {
				// A nil controllable ensures no interaction reaches player methods.
				if err := (&InteractHandler{}).Handle(&packet.Interact{ActionType: action, TargetEntityRuntimeID: v.id}, s, tx, nil); err != nil {
					t.Fatal(err)
				}
			}
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkEntityViewSnapshots(b *testing.B) {
	for _, count := range []int{1, 50, 150} {
		b.Run(fmt.Sprintf("viewers=%d", count), func(b *testing.B) {
			definitions := []world.EntityPropertyDefinition{{Name: "test:revision", Type: world.EntityPropertyInt, Min: 0, Max: 100}}
			for i := range 11 {
				definitions = append(definitions, world.EntityPropertyDefinition{Name: fmt.Sprintf("test:float_%d", i), Type: world.EntityPropertyFloat, Min: -4096, Max: 4096})
			}
			owner, w := newEntityViewFixture(b, definitions...)
			sessions := make([]*Session, count)
			views := make([]*EntityView, count)
			state := EntityViewState{Properties: make([]world.EntityPropertyValue, 12)}
			state.Properties[0] = world.EntityPropertyValue{Type: world.EntityPropertyInt, Int: 1}
			for i := range 11 {
				state.Properties[i+1] = world.EntityPropertyValue{Type: world.EntityPropertyFloat}
			}
			if err := w.Do(func(tx *world.Tx) {
				for i := range views {
					s := newEnvironmentViewTestSession()
					s.ent = owner.ent // Isolate view/encoding cost, without ticking synthetic players.
					s.currentEntityRuntimeID = selfEntityRuntimeID
					sessions[i] = s
					var err error
					views[i], err = s.AddEntityView(tx, "test:display", state)
					if err != nil {
						b.Fatal(err)
					}
					for len(s.packets) > 0 {
						<-s.packets
					}
				}
			}).Wait(b.Context()); err != nil {
				b.Fatal(err)
			}
			var buf bytes.Buffer
			b.ReportAllocs()
			b.ResetTimer()
			for n := range b.N {
				state.Properties[0].Int = int32(n%2 + 1)
				if err := w.Do(func(tx *world.Tx) {
					for i, v := range views {
						if err := sessions[i].UpdateEntityView(tx, v, state); err != nil {
							b.Fatal(err)
						}
					}
				}).Wait(b.Context()); err != nil {
					b.Fatal(err)
				}
				for _, s := range sessions {
					for len(s.packets) > 0 {
						buf.Reset()
						(<-s.packets).Marshal(protocol.NewWriter(&buf, 0))
					}
				}
			}
			b.StopTimer()
			if err := w.Do(func(*world.Tx) {
				for _, s := range sessions {
					s.ClearEntityViews()
				}
			}).Wait(b.Context()); err != nil {
				b.Fatal(err)
			}
		})
	}
}

func TestEntityViewRemovalCancelsPendingSameWorldAndCrossWorldUpdates(t *testing.T) {
	s, w := newEntityViewFixture(t)
	destination := world.Config{Synchronous: true, Entities: w.EntityRegistry()}.New()
	t.Cleanup(func() { _ = destination.Close() })
	var v *EntityView
	if err := w.Do(func(tx *world.Tx) {
		var err error
		v, err = s.AddEntityView(tx, "test:display", testViewState(0))
		if err != nil {
			t.Fatal(err)
		}
		for len(s.packets) > 0 {
			<-s.packets
		}
		if err := s.UpdateEntityView(tx, v, testViewState(1)); err != nil {
			t.Fatal(err)
		}
		h := tx.RemoveEntity(s.entEntity(t, tx))
		if !v.Closed() {
			t.Fatal("view survived source detachment")
		}
		tx.AddEntity(h)
		if !errors.Is(s.UpdateEntityView(tx, v, testViewState(2)), ErrEntityViewClosed) {
			t.Fatal("same-world re-entry revived stale handle")
		}
		tx.RemoveEntity(s.entEntity(t, tx))
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	packetOf[*packet.RemoveActor](t, s)
	if len(s.packets) != 0 {
		t.Fatal("late state survived removal")
	}
	if err := destination.Do(func(tx *world.Tx) {
		tx.AddEntity(s.ent)
		if !errors.Is(s.UpdateEntityView(tx, v, testViewState(3)), ErrEntityViewClosed) {
			t.Fatal("destination revived stale handle")
		}
		if err := s.CloseEntityView(tx, v); err != nil {
			t.Fatal(err)
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (s *Session) entEntity(t *testing.T, tx *world.Tx) world.Entity {
	t.Helper()
	e, ok := s.ent.Entity(tx)
	if !ok {
		t.Fatal("owner is missing")
	}
	return e
}

func TestEntityViewInvalidInputAndDisconnect(t *testing.T) {
	if !(new(EntityView)).Closed() {
		t.Fatal("zero-value handle should be closed")
	}
	s, w := newEntityViewFixture(t)
	if err := w.Do(func(tx *world.Tx) {
		v, err := s.AddEntityView(tx, "test:display", testViewState(0))
		if err != nil {
			t.Fatal(err)
		}
		bad := testViewState(101)
		if s.UpdateEntityView(tx, v, bad) == nil {
			t.Fatal("accepted out-of-range value")
		}
		bad = testViewState(0)
		bad.Position[0] = math.NaN()
		if s.UpdateEntityView(tx, v, bad) == nil {
			t.Fatal("accepted NaN anchor")
		}
		other := newEnvironmentViewTestSession()
		if !errors.Is(other.UpdateEntityView(tx, v, testViewState(0)), ErrEntityViewClosed) {
			t.Fatal("another session updated display")
		}
		close(s.closeBackground)
		if !v.Closed() || !errors.Is(s.UpdateEntityView(tx, v, testViewState(1)), ErrEntityViewClosed) {
			t.Fatal("disconnected view still writable")
		}
		s.ClearEntityViews()
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
