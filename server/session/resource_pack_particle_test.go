package session

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestViewResourcePackParticle(t *testing.T) {
	s := newResourcePackParticleTestSession(t, world.Overworld)
	effect, err := particle.NewResourcePack("valor:storm_wall_x", map[string]float32{"variable.span": 24})
	if err != nil {
		t.Fatal(err)
	}
	position := mgl64.Vec3{12.5, 70, -8.25}
	s.ViewParticle(position, effect)

	pk := packetOf[*packet.SpawnParticleEffect](t, s)
	if pk.Dimension != 0 || pk.EntityUniqueID != -1 || pk.Position != vec64To32(position) || pk.ParticleName != effect.Identifier() {
		t.Fatalf("unexpected particle packet: %#v", pk)
	}
	variables, ok := pk.MoLangVariables.Value()
	if !ok || string(variables) != effect.MolangVariables() {
		t.Fatalf("Molang variables = %q/%v, want %q/true", variables, ok, effect.MolangVariables())
	}
}

func TestViewResourcePackParticleWithoutVariables(t *testing.T) {
	s := newResourcePackParticleTestSession(t, world.Overworld)
	effect, err := particle.NewResourcePack("valor:storm_wall_x", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.ViewParticle(mgl64.Vec3{}, effect)

	pk := packetOf[*packet.SpawnParticleEffect](t, s)
	if _, ok := pk.MoLangVariables.Value(); ok {
		t.Fatal("Molang variables were present")
	}
}

func TestViewResourcePackParticleDimensions(t *testing.T) {
	for _, test := range []struct {
		name string
		dim  world.Dimension
		want byte
	}{
		{name: "overworld", dim: world.Overworld, want: 0},
		{name: "nether", dim: world.Nether, want: 1},
		{name: "end", dim: world.End, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newResourcePackParticleTestSession(t, test.dim)
			effect, err := particle.NewResourcePack("valor:storm_wall", nil)
			if err != nil {
				t.Fatal(err)
			}
			s.ViewParticle(mgl64.Vec3{}, effect)
			if got := packetOf[*packet.SpawnParticleEffect](t, s).Dimension; got != test.want {
				t.Fatalf("dimension = %d, want %d", got, test.want)
			}
		})
	}
}

func TestViewResourcePackParticleWithoutWorld(t *testing.T) {
	s := newEntityViewTestSession()
	effect, err := particle.NewResourcePack("valor:storm_wall", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.ViewParticle(mgl64.Vec3{}, effect)
	assertNoEntityViewPacket(t, s)
}

func TestViewResourcePackParticleConcurrentReuse(t *testing.T) {
	s := newResourcePackParticleTestSession(t, world.Overworld)
	effect, err := particle.NewResourcePack("valor:storm_wall", map[string]float32{"variable.span": 24})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for index := range 64 {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			s.ViewParticle(mgl64.Vec3{float64(index), 64, 0}, effect)
		}(index)
	}
	workers.Wait()
	for range 64 {
		_ = packetOf[*packet.SpawnParticleEffect](t, s)
	}
}

func newResourcePackParticleTestSession(t *testing.T, dimension world.Dimension) *Session {
	t.Helper()
	s := newEntityViewTestSession()
	w := world.Config{Dim: dimension, Synchronous: true}.New()
	s.chunkLoader = world.NewLoader(0, w, s)
	for len(s.packets) != 0 {
		<-s.packets
	}
	t.Cleanup(func() {
		task := w.Do(func(tx *world.Tx) {
			s.chunkLoader.Close(tx)
		})
		<-task.Done()
		if err := w.Close(); err != nil {
			t.Errorf("close test world: %v", err)
		}
	})
	return s
}
