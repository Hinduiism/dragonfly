package player

import (
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

func TestOpenVirtualChestValidatesConfiguration(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		handle := world.EntitySpawnOpts{}.New(Type, Config{Name: "Virtual Container Test"})
		p := tx.AddEntity(handle).(*Player)
		if err := p.OpenVirtualChest(tx, VirtualContainerConfig{Inventory: inventory.New(10, nil)}); !errors.Is(err, ErrInvalidVirtualContainer) {
			t.Fatalf("invalid size error = %v, want ErrInvalidVirtualContainer", err)
		}
		if err := p.OpenVirtualChest(tx, VirtualContainerConfig{Inventory: inventory.New(27, nil)}); !errors.Is(err, ErrVirtualContainerUnavailable) {
			t.Fatalf("sessionless open error = %v, want ErrVirtualContainerUnavailable", err)
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}
