package session

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestSubChunkOmitsPhantomUnknownBlockActors(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	unknown := world.UnimplementedBlocks()
	if len(unknown) == 0 {
		t.Fatal("fixture needs an unimplemented block")
	}
	b := unknown[0]
	pos := cube.Pos{1, 64, 1}
	col := &world.Column{Chunk: chunk.New(w.BlockRegistry(), w.Range()), BlockEntities: make(map[cube.Pos]world.Block)}
	col.SetBlock(1, 64, 1, 0, w.BlockRegistry().BlockRuntimeID(b))
	s := &Session{conn: unknownNBTConn{}, br: w.BlockRegistry()}
	ind := col.SubIndex(64)
	terrain := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind))
	for _, data := range []map[string]any{nil, {}, {"x": int32(1), "y": int32(64), "z": int32(1)}} {
		col.BlockEntities[pos] = b.(world.NBTer).DecodeNBT(data).(world.Block)
		entry := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, nil)
		payload, ok := entry.RawPayload.Value()
		if !ok || !bytes.Equal(payload, terrain) {
			t.Fatalf("empty unknown actor added data to subchunk: terrain=%d payload=%d", len(terrain), len(payload))
		}
	}
	data := map[string]any{"id": "UnsupportedTile", "CustomPayload": "preserved"}
	col.BlockEntities[pos] = b.(world.NBTer).DecodeNBT(data).(world.Block)
	entry := s.subChunkEntry(protocol.SubChunkOffset{}, ind, col, nil)
	payload, _ := entry.RawPayload.Value()
	var decoded map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(payload[len(terrain):]), nbt.NetworkLittleEndian).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	data["x"], data["y"], data["z"] = int32(1), int32(64), int32(1)
	if !reflect.DeepEqual(decoded, data) {
		t.Fatalf("genuine unknown actor changed: %#v", decoded)
	}
}

type unknownNBTConn struct{ Conn }

func (unknownNBTConn) ClientCacheEnabled() bool { return false }
