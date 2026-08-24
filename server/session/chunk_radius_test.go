package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestEffectiveChunkRadiusCombinesGlobalAndWorldCaps(t *testing.T) {
	uncapped := world.Config{Synchronous: true}.New()
	capped := world.Config{Synchronous: true, MaxChunkRadius: 6}.New()
	defer uncapped.Close()
	defer capped.Close()

	tests := []struct {
		name              string
		requested, global int32
		world             *world.World
		want              int32
	}{
		{name: "uncapped", requested: 10, global: 12, world: uncapped, want: 10},
		{name: "global", requested: 20, global: 12, world: uncapped, want: 12},
		{name: "world", requested: 10, global: 12, world: capped, want: 6},
		{name: "both", requested: 20, global: 4, world: capped, want: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveChunkRadius(test.requested, test.global, test.world); got != test.want {
				t.Fatalf("effectiveChunkRadius() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSessionRestoresRequestedRadiusAfterLeavingCappedWorld(t *testing.T) {
	capped := world.Config{Synchronous: true, MaxChunkRadius: 4}.New()
	uncapped := world.Config{Synchronous: true}.New()
	defer capped.Close()
	defer uncapped.Close()

	s := &Session{requestedChunkRadius: 12, chunkRadius: 12, maxChunkRadius: 16}
	if !s.applyChunkRadius(s.requestedChunkRadius, capped) || s.chunkRadius != 4 {
		t.Fatalf("capped radius = %d, want 4", s.chunkRadius)
	}
	if s.requestedChunkRadius != 12 {
		t.Fatalf("requested radius overwritten with %d", s.requestedChunkRadius)
	}
	if !s.applyChunkRadius(s.requestedChunkRadius, uncapped) || s.chunkRadius != 12 {
		t.Fatalf("restored radius = %d, want 12", s.chunkRadius)
	}
}

func TestSessionRemembersRadiusRequestWhileWorldCapIsUnchanged(t *testing.T) {
	capped := world.Config{Synchronous: true, MaxChunkRadius: 4}.New()
	uncapped := world.Config{Synchronous: true}.New()
	defer capped.Close()
	defer uncapped.Close()

	s := &Session{requestedChunkRadius: 12, chunkRadius: 4, maxChunkRadius: 16}
	if changed := s.applyChunkRadius(14, capped); changed {
		t.Fatal("effective radius changed while the same cap remained active")
	}
	if s.requestedChunkRadius != 14 || s.chunkRadius != 4 {
		t.Fatalf("request/effective radius = %d/%d, want 14/4", s.requestedChunkRadius, s.chunkRadius)
	}
	s.applyChunkRadius(s.requestedChunkRadius, uncapped)
	if s.chunkRadius != 14 {
		t.Fatalf("restored radius = %d, want 14", s.chunkRadius)
	}
}
