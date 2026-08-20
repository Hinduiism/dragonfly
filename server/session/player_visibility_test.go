package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
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
