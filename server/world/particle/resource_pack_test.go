package particle

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestResourcePack(t *testing.T) {
	variables := map[string]float32{
		"variable.span":  24,
		"variable.alpha": 0.5,
	}
	value, err := NewResourcePack("valor:storm/wall_x", variables)
	if err != nil {
		t.Fatalf("new resource-pack particle: %v", err)
	}
	variables["variable.span"] = 1
	delete(variables, "variable.alpha")

	if got, want := value.Identifier(), "valor:storm/wall_x"; got != want {
		t.Fatalf("identifier = %q, want %q", got, want)
	}
	if got, want := value.MolangVariables(), `{"variable.alpha":0.5,"variable.span":24}`; got != want {
		t.Fatalf("variables = %q, want %q", got, want)
	}
}

func TestResourcePackWithoutVariables(t *testing.T) {
	value, err := NewResourcePack("valor:storm_wall", nil)
	if err != nil {
		t.Fatalf("new resource-pack particle: %v", err)
	}
	if got := value.MolangVariables(); got != "" {
		t.Fatalf("variables = %q, want empty", got)
	}
}

func TestResourcePackRejectsInvalidInput(t *testing.T) {
	identifiers := []string{
		"", "valor", ":wall", "valor:", "Valor:wall", "valor:Storm", "valor:../wall",
		"valor:wall space", "valor:wall:other", "valor\\wall",
	}
	for _, identifier := range identifiers {
		t.Run("identifier_"+identifier, func(t *testing.T) {
			if _, err := NewResourcePack(identifier, nil); err == nil {
				t.Fatalf("NewResourcePack(%q) succeeded", identifier)
			}
		})
	}

	variables := []map[string]float32{
		{"span": 1},
		{"variable.": 1},
		{"variable.bad-name": 1},
		{"variable.BAD": 1},
		{"variable.bad..name": 1},
		{"variable.bad": float32(math.NaN())},
		{"variable.bad": float32(math.Inf(1))},
	}
	for index, values := range variables {
		t.Run("variables_"+string(rune('a'+index)), func(t *testing.T) {
			if _, err := NewResourcePack("valor:wall", values); err == nil {
				t.Fatalf("NewResourcePack accepted %#v", values)
			}
		})
	}

	tooMany := make(map[string]float32, maxResourcePackVariables+1)
	for index := range maxResourcePackVariables + 1 {
		tooMany["variable.value_"+strings.Repeat("a", index+1)] = float32(index)
	}
	if _, err := NewResourcePack("valor:wall", tooMany); err == nil {
		t.Fatal("NewResourcePack accepted too many variables")
	}
}

func TestResourcePackEncodingIsDeterministic(t *testing.T) {
	left, err := NewResourcePack("valor:wall", map[string]float32{
		"variable.c": 3, "variable.a": 1, "variable.b": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewResourcePack("valor:wall", map[string]float32{
		"variable.b": 2, "variable.c": 3, "variable.a": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if left.MolangVariables() != right.MolangVariables() {
		t.Fatalf("encoding differs: %q != %q", left.MolangVariables(), right.MolangVariables())
	}
}

func TestResourcePackConcurrentReuse(t *testing.T) {
	value, err := NewResourcePack("valor:wall", map[string]float32{"variable.span": 24})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if value.Identifier() == "" || value.MolangVariables() == "" {
				t.Error("immutable value changed")
			}
		}()
	}
	workers.Wait()
}
