package player

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/session"
)

var (
	// ErrEntityViewUnavailable is returned when a player has no active network
	// session on which an entity view can be shown.
	ErrEntityViewUnavailable = session.ErrEntityViewUnavailable
	// ErrEntityViewClosed is returned when an operation targets an entity view
	// that has already been removed or invalidated.
	ErrEntityViewClosed = session.ErrEntityViewClosed
	// ErrEntityViewInvalid is returned when an entity view configuration or
	// transform contains an invalid value.
	ErrEntityViewInvalid = session.ErrEntityViewInvalid
	// ErrEntityViewUnregistered is returned when the configured identifier is
	// not registered in the player's current world.
	ErrEntityViewUnregistered = errors.New("entity view identifier is not registered")
	// ErrEntityViewUnsupported is returned when an identifier needs a
	// specialised spawn packet that visual entity views do not support.
	ErrEntityViewUnsupported = errors.New("entity view identifier is unsupported")
)

// EntityViewConfig describes a visual entity shown only to one Player.
// Identifier must be registered in the Player's current world entity registry.
type EntityViewConfig = session.EntityViewConfig

// EntityView is a visual-only entity shown to one Player. It is independent of
// world chunks, ticking, collision, and persistence.
type EntityView = session.EntityView

// AddEntityView shows a visual-only entity to p. The configured identifier
// must be registered in p's current world.
func (p *Player) AddEntityView(conf EntityViewConfig) (*EntityView, error) {
	if p == nil || p.session() == session.Nop || p.tx == nil || p.tx.World() == nil {
		return nil, ErrEntityViewUnavailable
	}
	t, ok := p.tx.World().EntityRegistry().Lookup(conf.Identifier)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrEntityViewUnregistered, conf.Identifier)
	}
	if unsupportedEntityViewType(t) {
		return nil, fmt.Errorf("%w: %q", ErrEntityViewUnsupported, conf.Identifier)
	}
	return p.session().AddEntityView(conf)
}

func unsupportedEntityViewType(t interface{ EncodeEntity() string }) bool {
	if _, ok := t.(interface{ NetworkEncodeEntity() string }); ok {
		return true
	}
	switch t.EncodeEntity() {
	case "minecraft:player", "minecraft:item", "minecraft:falling_block":
		return true
	default:
		return false
	}
}
