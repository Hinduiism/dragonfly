// Package camera describes client camera instructions without exposing packets.
package camera

import (
	"image/color"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

// View positions the free camera and points it at a world position. A zero
// duration snaps immediately; a positive duration uses out-cubic easing.
type View struct {
	Position, Facing mgl64.Vec3
	Duration         time.Duration
}

// Fade describes the three phases of a camera fade.
type Fade struct {
	In, Hold, Out time.Duration
	Colour        color.RGBA
}
