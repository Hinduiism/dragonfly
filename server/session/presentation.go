package session

import "github.com/df-mc/dragonfly/server/world"

// presentationSnapshot is immutable and safe for cross-owner list publication.
type presentationSnapshot struct {
	name     string
	revision uint64
}

func (s *Session) presentation() presentationSnapshot {
	if value := s.presentationName.Load(); value != nil {
		return *value
	}
	if s.conn != nil {
		return presentationSnapshot{name: s.conn.IdentityData().DisplayName}
	}
	return presentationSnapshot{}
}

// PublishPresentationName must be called with target open on its world owner.
// Other sessions are only accessed through the synchronized publication path.
func (s *Session) PublishPresentationName(target world.Entity, name string) {
	if s == nil || s == Nop || target == nil || s.ent != target.H() {
		return
	}
	previous := s.presentation()
	if previous.name == name {
		return
	}
	s.presentationName.Store(&presentationSnapshot{name: name, revision: previous.revision + 1})
	actor, ok := target.(playerActor)
	if !ok {
		return
	}
	for _, viewer := range sessions.snapshot() {
		// The target session is already known. Looking it up for every viewer
		// would turn one appearance update into a quadratic registry scan.
		viewer.refreshConnectedPlayer(actor, s)
	}
}

func presentationName(e interface{ Name() string }) string {
	if v, ok := e.(interface{ PresentationName() string }); ok {
		return v.PresentationName()
	}
	return e.Name()
}
