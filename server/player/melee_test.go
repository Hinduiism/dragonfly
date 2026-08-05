package player

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// Fixtures in this file are locked to PocketMine-MP commit
// 6a7cc02e9dff59b69241aa0bcffdb9903ce86beb.

func TestMeleeKnockBackMotion(t *testing.T) {
	tests := []struct {
		name             string
		victim, attacker mgl64.Vec3
		previous         mgl64.Vec3
		force, limit     float64
		want             mgl64.Vec3
		wantMotion       bool
	}{
		{
			name:   "cardinal",
			victim: mgl64.Vec3{1, 64, 0}, attacker: mgl64.Vec3{0, 64, 0},
			force: 0.4, limit: 0.4, want: mgl64.Vec3{0.4, 0.4, 0}, wantMotion: true,
		},
		{
			name:   "diagonal with negative previous y",
			victim: mgl64.Vec3{3, 64, 4}, attacker: mgl64.Vec3{0, 64, 0}, previous: mgl64.Vec3{0.2, -0.2, 0.4},
			force: 0.4, limit: 0.4, want: mgl64.Vec3{0.34, 0.3, 0.52}, wantMotion: true,
		},
		{
			name:   "positive previous y capped",
			victim: mgl64.Vec3{0, 64, 2}, attacker: mgl64.Vec3{0, 64, 0}, previous: mgl64.Vec3{0, 0.8, 0},
			force: 0.4, limit: 0.4, want: mgl64.Vec3{0, 0.4, 0.4}, wantMotion: true,
		},
		{
			name:   "zero horizontal separation",
			victim: mgl64.Vec3{2, 70, 3}, attacker: mgl64.Vec3{2, 64, 3}, previous: mgl64.Vec3{1, 1, 1},
			force: 0.4, limit: 0.4, wantMotion: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := meleeKnockBackMotion(tt.victim, tt.attacker, tt.previous, tt.force, tt.limit)
			if ok != tt.wantMotion {
				t.Fatalf("motion availability: got %v, want %v", ok, tt.wantMotion)
			}
			if ok && !got.ApproxEqualThreshold(tt.want, 1e-12) {
				t.Fatalf("motion: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectMeleeGate(t *testing.T) {
	state := newDirectMeleeState(false)
	state.tick(100)
	if got := state.classify(5); got != directMeleeCold {
		t.Fatalf("initial decision: got %v, want cold", got)
	}
	state.commit(directMeleeCold, 5)

	if got := state.classify(5); got != directMeleeWarmRejected {
		t.Fatalf("equal-base decision: got %v, want warm rejected", got)
	}
	if got := state.classify(4); got != directMeleeWarmRejected {
		t.Fatalf("weaker-base decision: got %v, want warm rejected", got)
	}
	if got := state.classify(6); got != directMeleeWarmStronger {
		t.Fatalf("stronger-base decision: got %v, want warm stronger", got)
	}
	state.commit(directMeleeWarmStronger, 6)
	if state.attackTime != directMeleeCooldownTicks {
		t.Fatalf("stronger hit reset gate: got %d, want %d", state.attackTime, directMeleeCooldownTicks)
	}

	state.tick(109)
	if state.attackTime != 1 {
		t.Fatalf("gate at tick 9: got %d, want 1", state.attackTime)
	}
	state.tick(110)
	if state.attackTime != 0 || state.classify(5) != directMeleeCold {
		t.Fatalf("gate at tick 10: attackTime=%d decision=%v", state.attackTime, state.classify(5))
	}
}

func TestSpawnProtectionAndMeleeMotionLifecycle(t *testing.T) {
	state := newDirectMeleeState(true)
	state.tick(20)
	if state.noDamageTicks != directMeleeSpawnProtectionTicks-1 {
		t.Fatalf("first protection tick: got %d, want %d", state.noDamageTicks, directMeleeSpawnProtectionTicks-1)
	}
	state.tick(79)
	if state.noDamageTicks != 0 {
		t.Fatalf("protection expiry: got %d, want 0", state.noDamageTicks)
	}

	state.recordMotion(mgl64.Vec3{0.2, 0.4, -0.1}, 79)
	state.recordJump(0.42, 79)
	if want := (mgl64.Vec3{0.2, 0.42, -0.1}); state.motion != want {
		t.Fatalf("jump motion: got %v, want %v", state.motion, want)
	}
	state.tick(80)
	if state.motion != (mgl64.Vec3{}) {
		t.Fatalf("next-tick motion was not cleared: %v", state.motion)
	}
	state.recordMotion(mgl64.Vec3{1, 2, 3}, 80)
	state.clearMotion(80)
	if state.motion != (mgl64.Vec3{}) {
		t.Fatalf("explicit motion clear: %v", state.motion)
	}
}

func TestDirectMeleeStateFreezesWhileDead(t *testing.T) {
	state := newDirectMeleeState(false)
	state.tick(100)
	state.commit(directMeleeCold, 5)

	// Player.Tick does not call state.tick while dead. Respawn establishes a new
	// baseline so those skipped ticks are not charged on the next living tick.
	state.noDamageTicks = directMeleeSpawnProtectionTicks
	state.resumeAt(200)
	state.tick(201)
	if state.attackTime != directMeleeCooldownTicks-1 {
		t.Fatalf("dead ticks reduced attack gate: got %d, want %d", state.attackTime, directMeleeCooldownTicks-1)
	}
	if state.noDamageTicks != directMeleeSpawnProtectionTicks-1 {
		t.Fatalf("dead ticks reduced spawn protection: got %d, want %d", state.noDamageTicks, directMeleeSpawnProtectionTicks-1)
	}
}

func TestWaterSurfaceHeight(t *testing.T) {
	tests := []struct {
		name    string
		depth   int
		falling bool
		want    float64
	}{
		{name: "source", depth: 8, want: 64.99999998888889},
		{name: "flowing", depth: 7, want: 64.88888887777778},
		{name: "shallow", depth: 1, want: 64.22222221111111},
		{name: "falling", depth: 1, falling: true, want: 64.99999998888889},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waterSurfaceHeight(64, tt.depth, tt.falling)
			if math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("water surface: got %.15f, want %.15f", got, tt.want)
			}
		})
	}
}
