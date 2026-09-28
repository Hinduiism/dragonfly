package session

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

var sessions = new(sessionList)

type sessionList struct {
	mu sync.Mutex
	s  []*Session
}

func (l *sessionList) Add(s *Session) {
	l.mu.Lock()
	others := slices.Clone(l.s)
	l.s = append(l.s, s)
	s.registered.Store(true)
	l.mu.Unlock()

	for _, other := range others {
		// Show all sessions to the new session and the new session to all
		// existing sessions.
		l.reconcilePlayerList(s, other)
		l.reconcilePlayerList(other, s)
	}
	// Show the new session to itself.
	l.reconcilePlayerList(s, s)
}

func (l *sessionList) Remove(s *Session, entity world.Entity) {
	l.mu.Lock()
	removedFrom := slices.Clone(l.s)
	l.s = sliceutil.DeleteVal(l.s, s)
	s.registered.Store(false)
	l.mu.Unlock()
	for _, other := range removedFrom {
		l.removePlayerListEntry(s, other)
	}
	s.publicationMu.Lock()
	clear(s.playerList)
	clear(s.playerActors)
	s.publicationMu.Unlock()

	if entity == nil {
		return
	}
	for _, other := range removedFrom {
		if other.viewLayer != nil {
			other.viewLayer.Remove(entity)
		}
	}
}

func (l *sessionList) LookupHandle(handle *world.EntityHandle) (*Session, bool) {
	if handle == nil {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, current := range l.s {
		if current.ent == handle {
			return current, true
		}
	}
	return nil, false
}

func (l *sessionList) snapshot() []*Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.s)
}

func (l *sessionList) Lookup(id uuid.UUID) (*Session, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index := slices.IndexFunc(l.s, func(session *Session) bool {
		return session.ent.UUID() == id
	}); index != -1 {
		return l.s[index], true
	}
	return nil, false
}
