package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSnowballHitUsesPlayerMeleeKnockBack(t *testing.T) {
	for _, test := range []struct {
		name     string
		previous mgl64.Vec3
		want     mgl64.Vec3
	}{
		{"stationary", mgl64.Vec3{}, mgl64.Vec3{0.4, 0.4, 0}},
		{"blend previous motion", mgl64.Vec3{0.2, -0.2, 0.4}, mgl64.Vec3{0.5, 0.3, 0.2}},
		{"cap upward motion", mgl64.Vec3{0, 0.8, 0}, mgl64.Vec3{0.4, 0.4, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				victim.SetVelocity(test.previous)
				projectile := tx.AddEntity(entity.NewSnowball(projectileKnockBackOpts(), owner)).(*entity.Ent)
				projectile.Behaviour().(*entity.ProjectileBehaviour).Tick(projectile, tx)
				if got := victim.Velocity(); !got.ApproxEqualThreshold(test.want, 1e-12) {
					t.Fatalf("snowball motion = %v, want %v", got, test.want)
				}
				if victim.Health() != 20 {
					t.Fatalf("snowball changed health to %v", victim.Health())
				}
			})
		})
	}
}

func TestRejectedSnowballHitDoesNotApplyKnockBack(t *testing.T) {
	for _, reason := range []string{"cancelled damage", "projectile immunity"} {
		t.Run(reason, func(t *testing.T) {
			withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
				previous := mgl64.Vec3{0.2, -0.2, 0.4}
				victim.SetVelocity(previous)
				if reason == "cancelled damage" {
					victim.Handle(cancelSnowballDamage{})
				} else {
					victim.projectileImmuneUntil = time.Now().Add(time.Minute)
					victim.projectileLastDamage = 1
				}
				projectile := tx.AddEntity(entity.NewSnowball(projectileKnockBackOpts(), owner)).(*entity.Ent)
				projectile.Behaviour().(*entity.ProjectileBehaviour).Tick(projectile, tx)
				if got := victim.Velocity(); got != previous {
					t.Fatalf("rejected hit changed motion to %v, want %v", got, previous)
				}
			})
		})
	}
}

func TestArrowHitRetainsGenericKnockBack(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
		victim.SetVelocity(mgl64.Vec3{0.2, -0.2, 0.4})
		projectile := tx.AddEntity(entity.NewArrow(projectileKnockBackOpts(), owner)).(*entity.Ent)
		projectile.Behaviour().(*entity.ProjectileBehaviour).Tick(projectile, tx)
		want := mgl64.Vec3{0.45, 0.3608, 0}
		if got := victim.Velocity(); !got.ApproxEqualThreshold(want, 1e-12) {
			t.Fatalf("arrow motion = %v, want %v", got, want)
		}
	})
}

func TestSnowballWithoutPlayerOwnerRetainsGenericKnockBack(t *testing.T) {
	withProjectileKnockBackPlayers(t, func(tx *world.Tx, owner, victim *Player) {
		projectile := tx.AddEntity(entity.NewSnowball(projectileKnockBackOpts(), owner))
		victim.SetVelocity(mgl64.Vec3{0.2, -0.2, 0.4})
		victim.KnockBackProjectile(entity.ProjectileDamageSource{Projectile: projectile}, mgl64.Vec3{1.5, 0, 0}, 0.45, 0.3608)
		want := mgl64.Vec3{0.45, 0.3608, 0}
		if got := victim.Velocity(); !got.ApproxEqualThreshold(want, 1e-12) {
			t.Fatalf("unowned snowball motion = %v, want %v", got, want)
		}
	})
}

type cancelSnowballDamage struct{ NopHandler }

func (cancelSnowballDamage) HandleHurt(ctx *Context, _ *float64, _ bool, _ *time.Duration, _ world.DamageSource) {
	ctx.Cancel()
}

func projectileKnockBackOpts() world.EntitySpawnOpts {
	return world.EntitySpawnOpts{Position: mgl64.Vec3{0, 65, 0}, Velocity: mgl64.Vec3{1.5, 0, 0}}
}

func withProjectileKnockBackPlayers(t *testing.T, test func(*world.Tx, *Player, *Player)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	config := Config{Health: 20, MaxHealth: 20, GameMode: world.GameModeSurvival}
	if err := w.Do(func(tx *world.Tx) {
		config.Position = mgl64.Vec3{-8, 64, 0}
		owner := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, config)).(*Player)
		config.Position = mgl64.Vec3{1, 64, 0}
		victim := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, config)).(*Player)
		test(tx, owner, victim)
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
