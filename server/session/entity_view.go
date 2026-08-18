package session

import (
	"errors"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var (
	// ErrEntityViewUnavailable is returned when an entity view cannot be added
	// because its player has no active network session.
	ErrEntityViewUnavailable = errors.New("entity view is unavailable")
	// ErrEntityViewClosed is returned when an operation targets an entity view
	// that has already been removed or invalidated.
	ErrEntityViewClosed = errors.New("entity view is closed")
	// ErrEntityViewInvalid is returned when an entity view configuration or
	// transform contains an invalid value.
	ErrEntityViewInvalid = errors.New("invalid entity view")
)

// EntityViewConfig describes a visual entity shown only to one connection.
// Identifier is the encoded identifier of an ordinary actor registered in the
// player's current world.
type EntityViewConfig struct {
	Identifier        string
	Position          mgl64.Vec3
	Rotation          cube.Rotation
	Velocity          mgl64.Vec3
	Bounds            cube.BBox
	Scale             float64
	Immobile          bool
	NameTag           string
	AlwaysShowNameTag bool
}

// EntityView is a visual-only entity shown to one connection. It is not an
// authoritative world entity and has no collision, ticking, or persistence.
type EntityView struct {
	s  *Session
	id uint64
}

type entityViewState struct {
	position mgl64.Vec3
	rotation cube.Rotation
	onGround bool
}

// AddEntityView adds a visual-only entity to the session. Callers outside the
// session package should generally use player.Player.AddEntityView, which also
// validates the identifier against the current world's entity registry.
func (s *Session) AddEntityView(conf EntityViewConfig) (*EntityView, error) {
	conf, err := normaliseEntityViewConfig(conf)
	if err != nil {
		return nil, err
	}
	if s == nil || s == Nop || s.packets == nil || s.closeBackground == nil {
		return nil, ErrEntityViewUnavailable
	}

	s.entityViewsMu.Lock()
	defer s.entityViewsMu.Unlock()
	if s.entityViews == nil || s.closed() {
		return nil, ErrEntityViewUnavailable
	}

	// Lock ordering is always entityViewsMu followed by entityMutex. No normal
	// entity path acquires entityViewsMu while holding entityMutex.
	s.entityMutex.Lock()
	s.currentEntityRuntimeID++
	id := s.currentEntityRuntimeID
	s.entityMutex.Unlock()

	s.entityViews[id] = entityViewState{position: conf.Position, rotation: conf.Rotation}
	s.writeAddActor(id, conf.Identifier, entityViewMetadata(conf), conf.Position, conf.Velocity, conf.Rotation)
	return &EntityView{s: s, id: id}, nil
}

// Move updates the view using normal interpolated actor movement.
func (v *EntityView) Move(pos mgl64.Vec3, rot cube.Rotation, onGround bool) error {
	return v.move(pos, rot, onGround, false)
}

// Teleport updates the view without client interpolation.
func (v *EntityView) Teleport(pos mgl64.Vec3, rot cube.Rotation, onGround bool) error {
	return v.move(pos, rot, onGround, true)
}

func (v *EntityView) move(pos mgl64.Vec3, rot cube.Rotation, onGround, teleport bool) error {
	if !finiteVec3(pos) || !finiteRotation(rot) {
		return fmt.Errorf("%w: transform must contain only finite values", ErrEntityViewInvalid)
	}
	if v == nil || v.s == nil {
		return ErrEntityViewClosed
	}

	s := v.s
	s.entityViewsMu.Lock()
	defer s.entityViewsMu.Unlock()
	state, ok := s.entityViews[v.id]
	if !ok || s.closed() {
		return ErrEntityViewClosed
	}
	if state.position == pos && state.rotation == rot && state.onGround == onGround {
		return nil
	}
	state.position, state.rotation, state.onGround = pos, rot, onGround
	s.entityViews[v.id] = state
	s.writeActorAbsoluteMovement(v.id, pos, rot, onGround, teleport)
	return nil
}

// Close removes the view. Repeated calls are safe.
func (v *EntityView) Close() error {
	if v == nil || v.s == nil {
		return nil
	}
	s := v.s
	s.entityViewsMu.Lock()
	defer s.entityViewsMu.Unlock()
	if _, ok := s.entityViews[v.id]; !ok {
		return nil
	}
	delete(s.entityViews, v.id)
	s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(v.id)})
	return nil
}

// clearEntityViews invalidates and removes every visual entity owned by the
// session. It is called before world switches and during session shutdown.
func (s *Session) clearEntityViews() {
	if s == nil || s == Nop {
		return
	}
	s.entityViewsMu.Lock()
	defer s.entityViewsMu.Unlock()
	for id := range s.entityViews {
		s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(id)})
		delete(s.entityViews, id)
	}
}

// entityViewRuntimeID reports whether id belongs to an active visual entity.
func (s *Session) entityViewRuntimeID(id uint64) bool {
	s.entityViewsMu.Lock()
	_, ok := s.entityViews[id]
	s.entityViewsMu.Unlock()
	return ok
}

func (s *Session) closed() bool {
	select {
	case <-s.closeBackground:
		return true
	default:
		return false
	}
}

func normaliseEntityViewConfig(conf EntityViewConfig) (EntityViewConfig, error) {
	if conf.Identifier == "" {
		return conf, fmt.Errorf("%w: identifier is empty", ErrEntityViewInvalid)
	}
	if !finiteVec3(conf.Position) || !finiteRotation(conf.Rotation) || !finiteVec3(conf.Velocity) ||
		!finiteVec3(conf.Bounds.Min()) || !finiteVec3(conf.Bounds.Max()) {
		return conf, fmt.Errorf("%w: configuration must contain only finite values", ErrEntityViewInvalid)
	}
	if conf.Scale == 0 {
		conf.Scale = 1
	}
	if conf.Scale < 0 || math.IsNaN(conf.Scale) || math.IsInf(conf.Scale, 0) {
		return conf, fmt.Errorf("%w: scale must be finite and non-negative", ErrEntityViewInvalid)
	}
	return conf, nil
}

func entityViewMetadata(conf EntityViewConfig) protocol.EntityMetadata {
	m := protocol.NewEntityMetadata()
	m[protocol.EntityDataKeyWidth] = float32(conf.Bounds.Width())
	m[protocol.EntityDataKeyHeight] = float32(conf.Bounds.Height())
	m[protocol.EntityDataKeyEffectColor] = int32(0)
	m[protocol.EntityDataKeyEffectAmbience] = byte(0)
	m[protocol.EntityDataKeyColorIndex] = byte(0)
	m[protocol.EntityDataKeyScale] = float32(conf.Scale)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasGravity)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagClimb)
	if conf.Immobile {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI)
	}
	writeNameTagMetadata(m, conf.NameTag, conf.AlwaysShowNameTag)
	return m
}

func finiteVec3(v mgl64.Vec3) bool {
	return finite(v[0]) && finite(v[1]) && finite(v[2])
}

func finiteRotation(r cube.Rotation) bool {
	return finite(r.Yaw()) && finite(r.Pitch())
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
