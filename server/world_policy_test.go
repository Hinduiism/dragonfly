package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestConfigPassesWorldMaximumChunkRadius(t *testing.T) {
	srv := Config{
		Log:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
		DisableResourceBuilding: true,
		WorldMaxChunkRadius:     7,
		WorldTickPolicy: world.TickPolicy{
			Disabled: world.TickLightning,
		},
		WorldEntityStorage: world.EntityStorageTransient,
	}.New()
	defer srv.World().Close()
	defer srv.Nether().Close()
	defer srv.End().Close()

	for name, w := range map[string]*world.World{
		"overworld": srv.World(),
		"nether":    srv.Nether(),
		"end":       srv.End(),
	} {
		if got := w.MaxChunkRadius(); got != 7 {
			t.Fatalf("%s maximum chunk radius = %d, want 7", name, got)
		}
	}
}
