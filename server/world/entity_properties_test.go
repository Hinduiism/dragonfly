package world

import (
	"math"
	"testing"
)

func TestEntityPropertiesValidateAndCopy(t *testing.T) {
	defs := []EntityPropertyDefinition{{Name: "test:revision", Type: EntityPropertyInt, Min: 0, Max: 100}, {Name: "test:position", Type: EntityPropertyFloat, Min: -100, Max: 100}}
	schema, err := NewEntityPropertySchema(defs...)
	if err != nil {
		t.Fatal(err)
	}
	defs[0].Max = 0
	copy := schema.Definitions()
	copy[1].Name = "changed"
	values := []EntityPropertyValue{{Type: EntityPropertyInt, Int: 1}, {Type: EntityPropertyFloat, Float: 50}}
	if err := schema.Validate(values); err != nil {
		t.Fatal(err)
	}
	if schema.Definitions()[1].Name != "test:position" {
		t.Fatal("schema is mutable")
	}
	for _, bad := range [][]EntityPropertyValue{nil, values[:1], {{Type: EntityPropertyInt, Int: 101}, values[1]}, {values[0], {Type: EntityPropertyFloat, Float: float32(math.NaN())}}, {values[0], {Type: EntityPropertyInt}}} {
		if schema.Validate(bad) == nil {
			t.Fatalf("accepted invalid snapshot: %v", bad)
		}
	}
}

func TestEntityPropertiesRejectInvalidDefinitions(t *testing.T) {
	for _, defs := range [][]EntityPropertyDefinition{
		{{Name: "bad"}}, {{Name: "test:a"}, {Name: "test:a"}},
		{{Name: "test:a", Min: 1, Max: 0}}, {{Name: "test:a", Min: 0.5, Max: 1}},
		{{Name: "test:a", Type: 8}}, {{Name: "test:a", Type: EntityPropertyFloat, Max: math.Inf(1)}},
		make([]EntityPropertyDefinition, 33),
	} {
		if _, err := NewEntityPropertySchema(defs...); err == nil {
			t.Fatalf("accepted definitions: %v", defs)
		}
	}
}
