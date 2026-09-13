package world

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// EntityPropertyType identifies the wire type of a synchronized entity property.
type EntityPropertyType uint8

const (
	EntityPropertyInt EntityPropertyType = iota
	EntityPropertyFloat
)

// EntityPropertyDefinition describes a namespaced, client-synchronized number.
// Integer limits must be whole int32 values. Definitions share one index space.
type EntityPropertyDefinition struct {
	Name     string
	Type     EntityPropertyType
	Min, Max float64
}

// EntityPropertyValue is one value in schema order. Only the field matching Type
// may be nonzero. Updates must supply every property, including zero values.
type EntityPropertyValue struct {
	Type  EntityPropertyType
	Int   int32
	Float float32
}

// EntityPropertyDefiner is an optional EntityType extension. The registry takes
// an immutable schema snapshot when constructed; no per-entity callbacks run.
type EntityPropertyDefiner interface {
	EntityProperties() EntityPropertySchema
}

// EntityPropertySchema is an immutable ordered property definition. The zero
// value describes an entity without synchronized properties.
type EntityPropertySchema struct{ definitions []EntityPropertyDefinition }

// NewEntityPropertySchema validates and copies definitions. Bedrock supports at
// most 32 properties per entity type.
func NewEntityPropertySchema(definitions ...EntityPropertyDefinition) (EntityPropertySchema, error) {
	if len(definitions) > 32 {
		return EntityPropertySchema{}, fmt.Errorf("entity properties: more than 32 definitions")
	}
	seen := make(map[string]bool, len(definitions))
	for _, d := range definitions {
		ns, name, ok := strings.Cut(d.Name, ":")
		if !ok || ns == "" || name == "" || strings.ContainsAny(d.Name, " \t\r\n") || seen[d.Name] {
			return EntityPropertySchema{}, fmt.Errorf("entity properties: invalid or duplicate name %q", d.Name)
		}
		seen[d.Name] = true
		if math.IsNaN(d.Min) || math.IsNaN(d.Max) || math.IsInf(d.Min, 0) || math.IsInf(d.Max, 0) || d.Min > d.Max {
			return EntityPropertySchema{}, fmt.Errorf("entity property %s: invalid range", d.Name)
		}
		switch d.Type {
		case EntityPropertyInt:
			if d.Min < math.MinInt32 || d.Max > math.MaxInt32 || math.Trunc(d.Min) != d.Min || math.Trunc(d.Max) != d.Max {
				return EntityPropertySchema{}, fmt.Errorf("entity property %s: invalid integer range", d.Name)
			}
		case EntityPropertyFloat:
			if d.Min < -math.MaxFloat32 || d.Max > math.MaxFloat32 {
				return EntityPropertySchema{}, fmt.Errorf("entity property %s: invalid float range", d.Name)
			}
		default:
			return EntityPropertySchema{}, fmt.Errorf("entity property %s: unsupported type", d.Name)
		}
	}
	return EntityPropertySchema{definitions: slices.Clone(definitions)}, nil
}

// Definitions returns an independent copy, in wire index order.
func (s EntityPropertySchema) Definitions() []EntityPropertyDefinition {
	return slices.Clone(s.definitions)
}

// Equal reports whether schemas have identical ordered definitions.
func (s EntityPropertySchema) Equal(other EntityPropertySchema) bool {
	return slices.Equal(s.definitions, other.definitions)
}

// Validate checks a complete snapshot without retaining it.
func (s EntityPropertySchema) Validate(values []EntityPropertyValue) error {
	if len(values) != len(s.definitions) {
		return fmt.Errorf("entity properties: expected %d values, got %d", len(s.definitions), len(values))
	}
	for i, d := range s.definitions {
		v := values[i]
		if v.Type != d.Type {
			return fmt.Errorf("entity property %s: wrong value type", d.Name)
		}
		value := float64(v.Int)
		if d.Type == EntityPropertyFloat {
			if v.Int != 0 {
				return fmt.Errorf("entity property %s: integer field in float value", d.Name)
			}
			value = float64(v.Float)
		} else if v.Float != 0 {
			return fmt.Errorf("entity property %s: float field in integer value", d.Name)
		}
		low, high := d.Min, d.Max
		if d.Type == EntityPropertyFloat {
			low, high = float64(float32(low)), float64(float32(high))
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < low || value > high {
			return fmt.Errorf("entity property %s: value outside range", d.Name)
		}
	}
	return nil
}
