package player

import (
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

func TestAddEntityViewValidatesWorldRegistry(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		p := &Player{tx: tx, playerData: &playerData{s: &session.Session{}}}

		if _, err := p.AddEntityView(EntityViewConfig{Identifier: "dragonfly:not_registered"}); !errors.Is(err, ErrEntityViewUnregistered) {
			t.Fatalf("unregistered identifier: got %v, want ErrEntityViewUnregistered", err)
		}
		if _, err := p.AddEntityView(EntityViewConfig{Identifier: "minecraft:item"}); !errors.Is(err, ErrEntityViewUnsupported) {
			t.Fatalf("specialised identifier: got %v, want ErrEntityViewUnsupported", err)
		}
	})
}

func TestAddEntityViewRequiresActiveSession(t *testing.T) {
	if _, err := (*Player)(nil).AddEntityView(EntityViewConfig{}); !errors.Is(err, ErrEntityViewUnavailable) {
		t.Fatalf("nil player: got %v, want ErrEntityViewUnavailable", err)
	}
	p := &Player{playerData: &playerData{}}
	if _, err := p.AddEntityView(EntityViewConfig{}); !errors.Is(err, ErrEntityViewUnavailable) {
		t.Fatalf("player without session: got %v, want ErrEntityViewUnavailable", err)
	}
}
