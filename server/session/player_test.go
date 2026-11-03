package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type inputLockEntityType struct{ visibilityEntityType }

func (inputLockEntityType) NetworkOffset() float64 { return 1.621 }

type inputLockControllable struct {
	Controllable
	handle   *world.EntityHandle
	position mgl64.Vec3
}

func (c inputLockControllable) H() *world.EntityHandle { return c.handle }
func (c inputLockControllable) Position() mgl64.Vec3   { return c.position }

func TestInputLocksCaptureNetworkPositionAtLockAndUnlock(t *testing.T) {
	s := &Session{packets: make(chan packet.Packet, 3)}
	handle := world.EntitySpawnOpts{}.New(inputLockEntityType{}, visibilityEntityConfig{})
	defer handle.Close()
	c := inputLockControllable{handle: handle, position: mgl64.Vec3{123.5, 90, -55.5}}
	s.LockInput(input.Movement())
	s.SendInputLocks(c)
	locked := (<-s.packets).(*packet.UpdateClientInputLocks)
	want := mgl32.Vec3{123.5, float32(90 + 1.621), -55.5}
	if locked.Locks != packet.ClientInputLockMovement || locked.Position != want {
		t.Fatalf("lock=%+v, want movement lock at %v", locked, want)
	}
	// A respawn can move the player while locked. Unlock must use the new
	// position, and the already-queued lock must keep its original snapshot.
	c.position = mgl64.Vec3{4.5, 64, 0.5}
	s.UnlockInput(input.Movement())
	s.SendInputLocks(c)
	unlocked := (<-s.packets).(*packet.UpdateClientInputLocks)
	if unlocked.Locks != 0 || unlocked.Position != (mgl32.Vec3{4.5, float32(64 + 1.621), 0.5}) || locked.Position != want {
		t.Fatalf("unlock=%+v, previous lock=%+v", unlocked, locked)
	}
	s.LockInput(input.Camera())
	s.ClearInputLocks()
	s.SendInputLocks(c)
	cleared := (<-s.packets).(*packet.UpdateClientInputLocks)
	if cleared.Locks != 0 || cleared.Position != unlocked.Position {
		t.Fatalf("clear lost current position: %+v", cleared)
	}
}

type foodStatsControllable struct{ Controllable }

func (foodStatsControllable) Food() int                          { return 11 }
func (foodStatsControllable) FoodStats() (int, float64, float64) { return 11, 6.5, 2.25 }

func TestSpawnFoodSyncIncludesSaturationAndExhaustion(t *testing.T) {
	s := &Session{packets: make(chan packet.Packet, 1)}
	s.syncFood(foodStatsControllable{})
	update := (<-s.packets).(*packet.UpdateAttributes)
	want := map[string]float32{"minecraft:player.hunger": 11, "minecraft:player.saturation": 6.5, "minecraft:player.exhaustion": 2.25}
	for _, attribute := range update.Attributes {
		if attribute.Value != want[attribute.Name] {
			t.Fatalf("%s = %v, want %v", attribute.Name, attribute.Value, want[attribute.Name])
		}
		delete(want, attribute.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing attributes: %v", want)
	}
}

func TestHeldItemIdentityRoundTripsCustomServerTags(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	original := item.NewStack(item.GoldenApple{}, 2).WithValue("protected", true).WithValue("head", int32(1)).WithCustomName("Golden Head").WithLore("Velaris")
	decoded := stackToItem(world.DefaultBlockRegistry, instanceFromItem(world.DefaultBlockRegistry, original).Stack)
	if !original.Comparable(decoded) {
		t.Fatal("ordinary protocol encoding changed custom item identity")
	}
}
