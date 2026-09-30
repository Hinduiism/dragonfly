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

// RefreshPlayerVisibility re-evaluates one exact connected target's actor and
// player-list publication for this viewer. Unlike ShowEntity, it does not
// change an explicit HideEntity marker. It returns false if either player is
// detached or publication could not be queued.
func (p *Player) RefreshPlayerVisibility(target *Player) bool {
	if p == nil || target == nil || p.session() == nil {
		return false
	}
	return p.session().RefreshPlayerVisibility(target)
}
