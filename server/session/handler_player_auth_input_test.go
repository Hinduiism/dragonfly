package session

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerAuthInputFeetPositionSubtractsInFloat64(t *testing.T) {
	got := playerAuthInputFeetPosition(mgl32.Vec3{2, 1.00325, -3})
	want := mgl64.Vec3{2, -0.6167, -3}
	if !got.ApproxEqualThreshold(want, 1e-12) {
		t.Fatalf("feet position: got %v, want %v", got, want)
	}
}

func TestRoundPlayerAuthInputPosition(t *testing.T) {
	tests := []struct {
		name string
		in   mgl64.Vec3
		want mgl64.Vec3
	}{
		{name: "positive", in: mgl64.Vec3{1.23456, 72.00004, 9.87654}, want: mgl64.Vec3{1.2346, 72, 9.8765}},
		{name: "negative", in: mgl64.Vec3{-1.23456, -0.00006, -9.87654}, want: mgl64.Vec3{-1.2346, -0.0001, -9.8765}},
		{name: "half away from zero", in: mgl64.Vec3{1.23445, -1.23445, 0}, want: mgl64.Vec3{1.2345, -1.2345, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundPlayerAuthInputPosition(tt.in)
			if !got.ApproxEqualThreshold(tt.want, 1e-12) {
				t.Fatalf("round position: got %v, want %v", got, tt.want)
			}
		})
	}
}
