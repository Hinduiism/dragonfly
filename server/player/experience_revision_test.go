package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestExperienceRevisionTracksAcceptedMutations(t *testing.T) {
	withExperienceRevisionPlayer(t, 25, func(p *Player) {
		if got := p.ExperienceRevision(); got != 0 {
			t.Fatalf("initial revision = %d, want 0", got)
		}

		p.AddExperience(5)
		assertExperienceRevision(t, p, 1)
		p.RemoveExperience(3)
		assertExperienceRevision(t, p, 2)
		p.SetExperienceLevel(p.ExperienceLevel())
		assertExperienceRevision(t, p, 3)
		p.SetExperienceProgress(p.ExperienceProgress())
		assertExperienceRevision(t, p, 4)
	})
}

func TestExperienceRevisionIgnoresCancelledGain(t *testing.T) {
	withExperienceRevisionPlayer(t, 25, func(p *Player) {
		before := p.Experience()
		p.Handle(cancelExperienceGainHandler{})

		if added := p.AddExperience(5); added != 0 {
			t.Fatalf("AddExperience() = %d, want 0", added)
		}
		if got := p.Experience(); got != before {
			t.Fatalf("experience = %d, want %d", got, before)
		}
		assertExperienceRevision(t, p, 0)
	})
}

func TestExperienceRevisionIgnoresRejectedSetters(t *testing.T) {
	withExperienceRevisionPlayer(t, 25, func(p *Player) {
		assertPanics(t, func() { p.SetExperienceLevel(-1) })
		assertExperienceRevision(t, p, 0)
		assertPanics(t, func() { p.SetExperienceProgress(2) })
		assertExperienceRevision(t, p, 0)
	})
}

func TestExperienceRevisionTracksDeathReset(t *testing.T) {
	withExperienceRevisionPlayer(t, 25, func(p *Player) {
		p.dropItems()

		if got := p.Experience(); got != 0 {
			t.Fatalf("experience after dropItems() = %d, want 0", got)
		}
		assertExperienceRevision(t, p, 1)
	})
}

func TestDisabledExperiencePreservesSavedBalance(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	defer runtime.Close()
	if err := runtime.Do(func(tx *world.Tx) {
		handle := world.EntitySpawnOpts{}.New(Type, Config{Name: "Disabled XP", Experience: 25, DisableExperience: true})
		p := tx.AddEntity(handle).(*Player)
		if p.CanCollectExperience() || p.CollectExperience(5) || p.AddExperience(5) != 0 {
			t.Error("gameplay XP was accepted")
		}
		p.RemoveExperience(5)
		p.SetExperienceLevel(20)
		p.SetExperienceProgress(0.5)
		p.dropItems()
		if p.Experience() != 0 || p.ExperienceLevel() != 0 || p.ExperienceProgress() != 0 {
			t.Error("gameplay XP remains available")
		}
		if got := p.Data().Experience; got != 25 {
			t.Errorf("saved XP = %d, want 25", got)
		}
		assertExperienceRevision(t, p, 0)
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type cancelExperienceGainHandler struct{ NopHandler }

func (cancelExperienceGainHandler) HandleExperienceGain(ctx *Context, _ *int) {
	ctx.Cancel()
}

func withExperienceRevisionPlayer(t *testing.T, experience int, run func(*Player)) {
	t.Helper()
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.Do(func(tx *world.Tx) {
		handle := world.EntitySpawnOpts{}.New(Type, Config{Name: "Experience Revision Test", Experience: experience})
		run(tx.AddEntity(handle).(*Player))
	}).Wait(t.Context()); err != nil {
		t.Fatalf("world transaction: %v", err)
	}
}

func assertExperienceRevision(t *testing.T, p *Player, want uint64) {
	t.Helper()
	if got := p.ExperienceRevision(); got != want {
		t.Fatalf("experience revision = %d, want %d", got, want)
	}
}

func assertPanics(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	run()
}
