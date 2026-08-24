package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct{}

// Handle ...
func (*RequestChunkRadiusHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.RequestChunkRadius)
	if pk.ChunkRadius < 1 {
		return fmt.Errorf("expected chunk radius of at least 1, got %v", pk.ChunkRadius)
	}
	if s.applyChunkRadius(pk.ChunkRadius, tx.World()) {
		s.chunkLoader.ChangeRadius(tx, int(s.chunkRadius))
	}

	s.writePacket(&packet.ChunkRadiusUpdated{ChunkRadius: s.chunkRadius})
	return nil
}
