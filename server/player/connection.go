package player

import "github.com/df-mc/dragonfly/server/session"

// Connection returns a read-only reference to this exact login, or nil for an
// offline player. The reference is safe to retain after leaving the world owner.
func (p *Player) Connection() *session.Connection { return p.session().Connection() }

// RefreshCommands republishes command completion metadata using the configured
// presentation policy. Call it on the player's world owner.
func (p *Player) RefreshCommands() *session.Delivery { return p.session().RefreshCommands(p) }
