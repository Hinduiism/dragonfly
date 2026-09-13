package player

import (
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// EntityViewState describes the position and complete properties of a display.
type EntityViewState = session.EntityViewState

// EntityView is one private decorative actor, owned by a player session and its
// current world membership. It has no collision, ticking, persistence or world
// membership. Update and Close require a live Player from its owner transaction.
type EntityView struct{ view *session.EntityView }

// AddEntityView creates a private display of a type registered in the world.
// Its immutable property schema is sent before its first spawn to this player.
func (p *Player) AddEntityView(identifier string, state EntityViewState) (*EntityView, error) {
	v, err := p.session().AddEntityView(p.tx, identifier, state)
	if err != nil {
		return nil, err
	}
	return &EntityView{view: v}, nil
}

// Update replaces the complete state. Multiple changes in one transaction are
// coalesced. Handles from a previous world membership or login are rejected.
func (v *EntityView) Update(p *Player, state EntityViewState) error {
	if v == nil || p == nil {
		return session.ErrEntityViewClosed
	}
	return p.session().UpdateEntityView(p.tx, v.view, state)
}

// Close removes the display. Closing an already invalidated handle is harmless.
func (v *EntityView) Close(p *Player) error {
	if v == nil || v.Closed() {
		return nil
	}
	if p == nil {
		return session.ErrEntityViewClosed
	}
	return p.session().CloseEntityView(p.tx, v.view)
}

// Closed may be queried from any goroutine. No player transaction is retained.
func (v *EntityView) Closed() bool { return v == nil || v.view.Closed() }

// BeforeWorldRemoval implements world.EntityWorldRemover. Session-local views
// must be invalidated while the source transaction still owns the player.
func (p *Player) BeforeWorldRemoval(tx *world.Tx) {
	_ = tx.World()
	p.session().ClearEntityViews()
}
