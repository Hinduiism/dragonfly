package session

import (
	"errors"
	"math"
	"slices"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var ErrEntityViewClosed = errors.New("entity view is closed or belongs to another session/world")

// EntityViewState is a complete display snapshot. Position is the actor anchor;
// rotation and scale stay fixed. Properties are copied on submission.
type EntityViewState struct {
	Position   mgl64.Vec3
	Properties []world.EntityPropertyValue
}

// EntityView is a session-private, non-interactive display, never a world entity.
// Mutations are confined to the viewer's current world transaction. Only Closed
// may be called from another goroutine.
type EntityView struct {
	session     *Session
	world       *world.World
	id          uint64
	schema      world.EntityPropertySchema
	state, sent EntityViewState
	pendingTx   *world.Tx
	closed      atomic.Bool
}

// Closed includes transport disconnection, even before owner teardown executes.
func (v *EntityView) Closed() bool {
	if v == nil || v.session == nil || v.closed.Load() {
		return true
	}
	select {
	case <-v.session.closeBackground:
		return true
	default:
		return false
	}
}

func (s *Session) entityViewOwner(tx *world.Tx) bool {
	if s == nil || s == Nop || tx == nil || s.ent == nil {
		return false
	}
	select {
	case <-s.closeBackground:
		return false
	default:
	}
	_, ok := s.ent.Entity(tx)
	return ok
}

// AddEntityView creates a private display of a registered type, at scale 1 with
// zero collision bounds. Call only on the player's current world owner.
func (s *Session) AddEntityView(tx *world.Tx, identifier string, state EntityViewState) (*EntityView, error) {
	if !s.entityViewOwner(tx) {
		return nil, ErrEntityViewClosed
	}
	registry := tx.World().EntityRegistry()
	if _, ok := registry.Lookup(identifier); !ok {
		return nil, errors.New("entity view type is not registered")
	}
	schema, _ := registry.EntityProperties(identifier)
	if err := validateEntityViewState(schema, state); err != nil {
		return nil, err
	}
	if err := s.sendEntityPropertySchema(identifier, schema); err != nil {
		return nil, err
	}
	s.entityMutex.Lock()
	s.currentEntityRuntimeID++
	id := s.currentEntityRuntimeID
	s.entityMutex.Unlock()
	state.Properties = slices.Clone(state.Properties)
	v := &EntityView{session: s, world: tx.World(), id: id, schema: schema, state: state, sent: state}
	if s.entityViews == nil {
		s.entityViews = make(map[uint64]*EntityView)
	}
	s.entityViews[id] = v
	m := protocol.NewEntityMetadata()
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI)
	m.UnsetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasGravity)
	m.UnsetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasCollision)
	m[protocol.EntityDataKeyWidth], m[protocol.EntityDataKeyHeight] = float32(0), float32(0)
	m[protocol.EntityDataKeyScale] = float32(1)
	s.writePacket(&packet.AddActor{EntityUniqueID: int64(id), EntityRuntimeID: id, EntityType: identifier, Position: vec64To32(state.Position), EntityMetadata: m, EntityProperties: entityPropertyValues(state.Properties)})
	return v, nil
}

// UpdateEntityView copies the latest state and coalesces writes until the end of
// this transaction. No extra timer, goroutine or transport flush is introduced.
func (s *Session) UpdateEntityView(tx *world.Tx, v *EntityView, state EntityViewState) error {
	if v.Closed() || v.session != s || !s.entityViewOwner(tx) || tx.World() != v.world {
		return ErrEntityViewClosed
	}
	if err := validateEntityViewState(v.schema, state); err != nil {
		return err
	}
	if sameEntityViewState(v.state, state) {
		return nil
	}
	state.Properties = slices.Clone(state.Properties)
	v.state = state
	if v.pendingTx != tx {
		v.pendingTx = tx
		tx.Defer(func(next *world.Tx) {
			// Removal invalidates the handle before another world can own it.
			if v.Closed() {
				return
			}
			if !s.entityViewOwner(next) || next.World() != v.world {
				return
			}
			v.pendingTx = nil
			if v.sent.Position != v.state.Position {
				s.writePacket(&packet.MoveActorAbsolute{EntityRuntimeID: v.id, Position: vec64To32(v.state.Position), Flags: packet.MoveFlagTeleport})
			}
			if !slices.Equal(v.sent.Properties, v.state.Properties) {
				s.writePacket(&packet.SetActorData{EntityRuntimeID: v.id, EntityMetadata: protocol.NewEntityMetadata(), EntityProperties: entityPropertyValues(v.state.Properties)})
			}
			v.sent = v.state
		})
	}
	return nil
}

// CloseEntityView removes a display idempotently on its owning player session.
func (s *Session) CloseEntityView(tx *world.Tx, v *EntityView) error {
	if v == nil || v.closed.Load() {
		return nil
	}
	if v.session != s || !s.entityViewOwner(tx) {
		return ErrEntityViewClosed
	}
	s.removeEntityView(v)
	return nil
}

func (s *Session) removeEntityView(v *EntityView) {
	if v.closed.Swap(true) {
		return
	}
	delete(s.entityViews, v.id)
	s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(v.id)})
}

// ClearEntityViews invalidates all world-bound views on the owner. This must run
// before detachment, including same-dimension transfers and same-world respawn.
func (s *Session) ClearEntityViews() {
	if s == nil || s == Nop {
		return
	}
	for _, v := range s.entityViews {
		s.removeEntityView(v)
	}
}

func validateEntityViewState(schema world.EntityPropertySchema, state EntityViewState) error {
	for _, coordinate := range state.Position {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || math.Abs(coordinate) > math.MaxFloat32 {
			return errors.New("entity view position must be finite")
		}
	}
	return schema.Validate(state.Properties)
}

func sameEntityViewState(a, b EntityViewState) bool {
	return a.Position == b.Position && slices.Equal(a.Properties, b.Properties)
}
