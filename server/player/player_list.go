package player

import "github.com/df-mc/dragonfly/server/world"

// RefreshPlayerListEntry re-evaluates one exact connected target's player-list
// membership for this viewer. It returns false if either player is detached or
// publication could not be queued.
func (p *Player) RefreshPlayerListEntry(target *world.EntityHandle) bool {
	if p == nil || target == nil || p.session() == nil {
		return false
	}
	return p.session().RefreshPlayerListEntry(target)
}

// RefreshPlayerList re-evaluates every connected target's player-list
// membership for this viewer. Detached players are safe no-ops.
func (p *Player) RefreshPlayerList() {
	if p == nil || p.session() == nil {
		return
	}
	p.session().RefreshPlayerList()
}
