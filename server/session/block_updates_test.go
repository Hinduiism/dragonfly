package session

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestViewBlockUpdatesUsesAbsoluteSubChunkOrigin(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	for _, test := range []struct {
		name   string
		chunk  world.SubChunkPos
		origin protocol.BlockPos
	}{
		{"positive", world.SubChunkPos{7, 4, 3}, protocol.BlockPos{112, 64, 48}},
		{"negative", world.SubChunkPos{-7, -4, -3}, protocol.BlockPos{-112, -64, -48}},
		{"zero", world.SubChunkPos{}, protocol.BlockPos{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newEnvironmentViewTestSession()
			s.br = w.BlockRegistry()
			position := cube.Pos{int(test.origin.X()) + 3, int(test.origin.Y()) + 5, int(test.origin.Z()) + 9}
			updates := []world.BlockUpdate{
				{Position: position, Block: block.Cobweb{}, Layer: 0},
				{Position: position, Block: block.Water{Depth: 8}, Layer: 1},
			}
			s.ViewBlockUpdates(test.chunk, updates)
			pk := packetOf[*packet.UpdateSubChunkBlocks](t, s)
			var wire bytes.Buffer
			pk.Marshal(protocol.NewWriter(&wire, 0))
			var decoded packet.UpdateSubChunkBlocks
			decoded.Marshal(protocol.NewReader(&wire, 0, true))
			if wire.Len() != 0 || decoded.Position != test.origin {
				t.Fatalf("packet origin = %v, want block origin %v (remaining=%d)", decoded.Position, test.origin, wire.Len())
			}
			if len(decoded.Blocks) != 1 || len(decoded.Extra) != 1 {
				t.Fatalf("layer lengths = %d/%d, want 1/1", len(decoded.Blocks), len(decoded.Extra))
			}
			for i, entry := range []protocol.BlockChangeEntry{decoded.Blocks[0], decoded.Extra[0]} {
				want := protocol.BlockPos{int32(position.X()), int32(position.Y()), int32(position.Z())}
				if entry.BlockPos != want || entry.BlockRuntimeID != s.br.BlockRuntimeID(updates[i].Block) || entry.Flags != packet.BlockUpdateNetwork {
					t.Errorf("layer %d lost absolute coordinates, runtime ID, or update flags: %+v", i, entry)
				}
			}
			assertNoEnvironmentPacket(t, s)
		})
	}
}
