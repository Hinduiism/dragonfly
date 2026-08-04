package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestKnockBackVelocity(t *testing.T) {
	tests := []struct {
		name                 string
		position, source     mgl64.Vec3
		current              mgl64.Vec3
		force, verticalLimit float64
		expected             mgl64.Vec3
		expectedApplication  bool
	}{
		{
			name:                "at rest",
			position:            mgl64.Vec3{3, 0, 4},
			force:               0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{0.24, 0.4, 0.32},
			expectedApplication: true,
		},
		{
			name:                "existing motion is halved",
			position:            mgl64.Vec3{3, 0, 4},
			current:             mgl64.Vec3{0.2, 0.1, -0.4},
			force:               0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{0.34, 0.4, 0.12},
			expectedApplication: true,
		},
		{
			name:                "jump motion is capped",
			position:            mgl64.Vec3{1, 0, 0},
			current:             mgl64.Vec3{0, 0.42, 0},
			force:               0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{0.4, 0.4, 0},
			expectedApplication: true,
		},
		{
			name:                "falling motion is preserved",
			position:            mgl64.Vec3{1, 0, 0},
			current:             mgl64.Vec3{0, -0.2, 0},
			force:               0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{0.4, 0.3, 0},
			expectedApplication: true,
		},
		{
			name:                "opposing horizontal motion is retained",
			position:            mgl64.Vec3{0, 0, 2},
			current:             mgl64.Vec3{0, 0, -0.6},
			force:               0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{0, 0.4, 0.1},
			expectedApplication: true,
		},
		{
			name:                "negative force pulls and lowers",
			position:            mgl64.Vec3{1, 0, 0},
			force:               -0.4,
			verticalLimit:       0.4,
			expected:            mgl64.Vec3{-0.4, -0.4, 0},
			expectedApplication: true,
		},
		{
			name:                "zero horizontal distance is rejected",
			position:            mgl64.Vec3{5, 10, 5},
			source:              mgl64.Vec3{5, -10, 5},
			force:               0.4,
			verticalLimit:       0.4,
			expectedApplication: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, applied := knockBackVelocity(test.position, test.source, test.current, test.force, test.verticalLimit)
			if applied != test.expectedApplication {
				t.Fatalf("knockBackVelocity() applied = %t, want %t", applied, test.expectedApplication)
			}
			if !applied {
				return
			}
			assertVec3Close(t, actual, test.expected)
		})
	}
}

func TestKnockBackVelocityConsecutiveApplication(t *testing.T) {
	position, source := mgl64.Vec3{1, 0, 0}, mgl64.Vec3{}
	first, ok := knockBackVelocity(position, source, mgl64.Vec3{}, 0.4, 0.4)
	if !ok {
		t.Fatal("first knockback was unexpectedly rejected")
	}
	second, ok := knockBackVelocity(position, source, first, 0.4, 0.4)
	if !ok {
		t.Fatal("second knockback was unexpectedly rejected")
	}
	assertVec3Close(t, first, mgl64.Vec3{0.4, 0.4, 0})
	assertVec3Close(t, second, mgl64.Vec3{0.6, 0.4, 0})
}

func TestImpactVelocityPreservesDragonflyCalculation(t *testing.T) {
	p := &Player{
		data:       &world.EntityData{Pos: mgl64.Vec3{3, 2, 4}},
		playerData: &playerData{armour: inventory.NewArmour(nil)},
	}
	assertVec3Close(t, p.impactVelocity(mgl64.Vec3{}, 0.5, 0.2), mgl64.Vec3{0.3, 0.2, 0.4})
}

func TestSetVelocityRetainsMotion(t *testing.T) {
	p := &Player{data: &world.EntityData{}, playerData: &playerData{}}
	expected := mgl64.Vec3{0.25, 0.4, -0.1}
	p.SetVelocity(expected)
	assertVec3Close(t, p.Velocity(), expected)
}

func TestKnockBackAllowed(t *testing.T) {
	tests := []struct {
		name             string
		resistance, roll float64
		expected         bool
	}{
		{name: "roll above resistance", resistance: 0.4, roll: 0.400001, expected: true},
		{name: "roll equals resistance", resistance: 0.4, roll: 0.4, expected: false},
		{name: "roll below resistance", resistance: 0.4, roll: 0.399999, expected: false},
		{name: "full resistance", resistance: 1, roll: 0.999999, expected: false},
		{name: "zero resistance", resistance: 0, roll: 0.5, expected: true},
		{name: "zero roll matches PocketMine boundary", resistance: 0, roll: 0, expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := knockBackAllowed(test.resistance, test.roll); actual != test.expected {
				t.Fatalf("knockBackAllowed(%v, %v) = %t, want %t", test.resistance, test.roll, actual, test.expected)
			}
		})
	}
}

func assertVec3Close(t *testing.T, actual, expected mgl64.Vec3) {
	t.Helper()
	for axis := range 3 {
		if math.Abs(actual[axis]-expected[axis]) > 1e-12 {
			t.Fatalf("vector = %v, want %v (axis %d differs)", actual, expected, axis)
		}
	}
}
