package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

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
