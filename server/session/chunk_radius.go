package session

import "github.com/df-mc/dragonfly/server/world"

// effectiveChunkRadius applies the server-wide and per-world chunk radius
// limits to the radius requested by the client.
func effectiveChunkRadius(requested, global int32, w *world.World) int32 {
	radius := requested
	if global > 0 && radius > global {
		radius = global
	}
	if w != nil {
		if cap := int32(w.MaxChunkRadius()); cap > 0 && radius > cap {
			radius = cap
		}
	}
	return radius
}

// applyChunkRadius records and applies the client's requested radius for the
// World. It reports whether the effective radius changed.
func (s *Session) applyChunkRadius(requested int32, w *world.World) bool {
	s.requestedChunkRadius = requested
	effective := effectiveChunkRadius(requested, s.maxChunkRadius, w)
	changed := effective != s.chunkRadius
	s.chunkRadius = effective
	return changed
}
