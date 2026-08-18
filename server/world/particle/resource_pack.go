package particle

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

const (
	maxResourcePackVariables = 32
	maxResourcePackPayload   = 4096
)

// ResourcePack is a particle effect defined by a client resource pack. It may
// include scalar Molang variables that are made available to the effect when
// it is spawned.
//
// ResourcePack values are immutable and may safely be reused by multiple
// viewers concurrently.
type ResourcePack struct {
	particle

	identifier string
	variables  string
}

// NewResourcePack returns a ResourcePack particle with the namespaced
// identifier and optional scalar Molang variables passed. Variable names must
// use the variable. prefix.
func NewResourcePack(identifier string, variables map[string]float32) (ResourcePack, error) {
	if !validResourcePackIdentifier(identifier) {
		return ResourcePack{}, fmt.Errorf("invalid resource-pack particle identifier %q", identifier)
	}
	encoded, err := encodeResourcePackVariables(variables)
	if err != nil {
		return ResourcePack{}, err
	}
	return ResourcePack{identifier: identifier, variables: encoded}, nil
}

// Identifier returns the namespaced resource-pack identifier of the particle.
func (p ResourcePack) Identifier() string { return p.identifier }

// MolangVariables returns the encoded scalar Molang variables of the particle.
// An empty string indicates that no variables were supplied.
func (p ResourcePack) MolangVariables() string { return p.variables }

func validResourcePackIdentifier(identifier string) bool {
	namespace, path, ok := strings.Cut(identifier, ":")
	if !ok || namespace == "" || path == "" || strings.Contains(path, ":") || strings.Contains(path, "..") {
		return false
	}
	for _, character := range namespace {
		if !resourceIdentifierCharacter(character, false) {
			return false
		}
	}
	for _, character := range path {
		if !resourceIdentifierCharacter(character, true) {
			return false
		}
	}
	return true
}

func resourceIdentifierCharacter(character rune, path bool) bool {
	if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
		return true
	}
	switch character {
	case '_', '-', '.':
		return true
	case '/':
		return path
	default:
		return false
	}
}

func encodeResourcePackVariables(variables map[string]float32) (string, error) {
	if len(variables) == 0 {
		return "", nil
	}
	if len(variables) > maxResourcePackVariables {
		return "", fmt.Errorf("resource-pack particle has %d Molang variables, maximum is %d", len(variables), maxResourcePackVariables)
	}

	keys := make([]string, 0, len(variables))
	for name, value := range variables {
		if !validMolangVariableName(name) {
			return "", fmt.Errorf("invalid resource-pack particle Molang variable %q", name)
		}
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("resource-pack particle Molang variable %q is not finite", name)
		}
		keys = append(keys, name)
	}
	slices.Sort(keys)

	encoded := make([]byte, 0, min(maxResourcePackPayload, len(keys)*32))
	encoded = append(encoded, '{')
	for index, name := range keys {
		if index != 0 {
			encoded = append(encoded, ',')
		}
		encoded = strconv.AppendQuote(encoded, name)
		encoded = append(encoded, ':')
		encoded = strconv.AppendFloat(encoded, float64(variables[name]), 'g', -1, 32)
	}
	encoded = append(encoded, '}')
	if len(encoded) > maxResourcePackPayload {
		return "", errors.New("resource-pack particle Molang payload is too large")
	}
	return string(encoded), nil
}

func validMolangVariableName(name string) bool {
	const prefix = "variable."
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) || len(name) > 128 || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	for _, character := range name[len(prefix):] {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
