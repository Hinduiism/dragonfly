package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestNaturalRegenerationUsesEightyTickCadence(t *testing.T) {
	withHungerTestPlayer(t, world.DifficultyNormal, Config{
		Health: 10, MaxHealth: 20, FoodTick: 1, Food: 20, Saturation: 20,
	}, func(p *Player) {
		p.tickFood()
		if got := p.Health(); got != 11 {
			t.Fatalf("health after first regeneration tick = %v, want 11", got)
		}

		for range 79 {
			p.tickFood()
		}
		if got := p.Health(); got != 11 {
			t.Fatalf("health before next 80-tick interval = %v, want 11", got)
		}

		p.tickFood()
		if got := p.Health(); got != 12 {
			t.Fatalf("health after next 80-tick interval = %v, want 12", got)
		}
	})
}

func TestNaturalRegenerationRequiresEighteenFood(t *testing.T) {
	withHungerTestPlayer(t, world.DifficultyNormal, Config{
		Health: 10, MaxHealth: 20, FoodTick: 1, Food: 17, Saturation: 17,
	}, func(p *Player) {
		p.tickFood()
		if got := p.Health(); got != 10 {
			t.Fatalf("health with 17 food = %v, want 10", got)
		}
	})
}

func TestNaturalRegenerationConsumesExhaustionOnlyAfterHealing(t *testing.T) {
	t.Run("heals", func(t *testing.T) {
		withHungerTestPlayer(t, world.DifficultyNormal, Config{
			Health: 10, MaxHealth: 20, FoodTick: 1, Food: 20, Saturation: 5,
		}, func(p *Player) {
			p.tickFood()
			if got := p.hunger.saturationLevel; got != 4 {
				t.Fatalf("saturation after healing = %v, want 4", got)
			}
			if got := p.hunger.exhaustionLevel; got != 2 {
				t.Fatalf("exhaustion after healing = %v, want 2", got)
			}
		})
	})

	t.Run("already full", func(t *testing.T) {
		withHungerTestPlayer(t, world.DifficultyNormal, Config{
			Health: 20, MaxHealth: 20, FoodTick: 1, Food: 20, Saturation: 5,
		}, func(p *Player) {
			p.tickFood()
			if got := p.hunger.saturationLevel; got != 5 {
				t.Fatalf("saturation at full health = %v, want 5", got)
			}
			if got := p.hunger.exhaustionLevel; got != 0 {
				t.Fatalf("exhaustion at full health = %v, want 0", got)
			}
		})
	})
}

func TestPeacefulFoodAndHealthRegenerationRemainEnabled(t *testing.T) {
	withHungerTestPlayer(t, world.DifficultyPeaceful, Config{
		Health: 10, MaxHealth: 20, FoodTick: 20, Food: 10, Saturation: 0,
	}, func(p *Player) {
		p.tickFood()
		if got := p.Food(); got != 11 {
			t.Fatalf("peaceful food after tick = %v, want 11", got)
		}
		if got := p.Health(); got != 11 {
			t.Fatalf("peaceful health after tick = %v, want 11", got)
		}
		if got := p.hunger.exhaustionLevel; got != 0 {
			t.Fatalf("peaceful exhaustion after healing = %v, want 0", got)
		}
	})
}

func withHungerTestPlayer(t *testing.T, difficulty world.Difficulty, config Config, test func(*Player)) {
	t.Helper()
	runtime := world.Config{Synchronous: true}.New()
	runtime.SetDifficulty(difficulty)
	t.Cleanup(func() { _ = runtime.Close() })
	handle := world.EntitySpawnOpts{}.New(Type, config)
	if err := runtime.Do(func(tx *world.Tx) {
		added := tx.AddEntity(handle)
		p, ok := added.(*Player)
		if !ok {
			t.Fatalf("added entity = %T, want *Player", added)
		}
		test(p)
	}).Wait(t.Context()); err != nil {
		t.Fatalf("world transaction error = %v", err)
	}
}
