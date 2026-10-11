package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestViewerHandleIdentifiesOnlyItsBoundEntity(t *testing.T) {
	var absent *Session
	if absent.ViewerHandle() != nil || (&Session{}).ViewerHandle() != nil {
		t.Fatal("an unbound session reported a viewer identity")
	}
	first, replacement := &world.EntityHandle{}, &world.EntityHandle{}
	s := &Session{ent: first}
	if s.ViewerHandle() != first || s.ViewerHandle() == replacement {
		t.Fatal("viewer identity was replaced by another handle")
	}
}
