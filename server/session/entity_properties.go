package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func (s *Session) sendEntityPropertySchema(identifier string, schema world.EntityPropertySchema) error {
	if previous, ok := s.entityPropertySchemas[identifier]; ok {
		if !previous.Equal(schema) {
			return fmt.Errorf("entity properties: schema changed for %s during session", identifier)
		}
		return nil
	}
	definitions := schema.Definitions()
	if len(definitions) != 0 {
		properties := make([]map[string]any, 0, len(definitions))
		for _, d := range definitions {
			var low, high any = float32(d.Min), float32(d.Max)
			if d.Type == world.EntityPropertyInt {
				low, high = int32(d.Min), int32(d.Max)
			}
			properties = append(properties, map[string]any{"name": d.Name, "type": int32(d.Type), "min": low, "max": high})
		}
		s.writePacket(&packet.SyncActorProperty{PropertyData: map[string]any{"type": identifier, "properties": properties}})
	}
	if s.entityPropertySchemas == nil {
		s.entityPropertySchemas = make(map[string]world.EntityPropertySchema)
	}
	s.entityPropertySchemas[identifier] = schema
	return nil
}

func entityPropertyValues(values []world.EntityPropertyValue) protocol.EntityProperties {
	p := protocol.EntityProperties{}
	for i, v := range values {
		if v.Type == world.EntityPropertyInt {
			p.IntegerProperties = append(p.IntegerProperties, protocol.IntegerEntityProperty{Index: uint32(i), Value: v.Int})
		} else {
			p.FloatProperties = append(p.FloatProperties, protocol.FloatEntityProperty{Index: uint32(i), Value: v.Float})
		}
	}
	return p
}
