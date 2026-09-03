package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestGameVersionWithoutSession(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		handle := world.EntitySpawnOpts{}.New(Type, Config{Name: "Version Test"})
		p := tx.AddEntity(handle).(*Player)
		if version := p.GameVersion(); version != "" {
			t.Fatalf("GameVersion() = %q, want empty string", version)
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatalf("world transaction: %v", err)
	}
}
