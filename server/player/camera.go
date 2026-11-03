package player

import (
	"github.com/df-mc/dragonfly/server/player/camera"
	"github.com/df-mc/dragonfly/server/session"
)

// RegisterCamera registers the free-camera preset on this connection. Call it
// once before SetCamera. Delivery completion means written, not client-ready.
func (p *Player) RegisterCamera() *session.Delivery { return p.session().RegisterCamera() }

// SetCamera positions the free camera. It shares the session's ordered output
// queue with teleports and all other Dragonfly output.
func (p *Player) SetCamera(view camera.View) *session.Delivery { return p.session().SetCamera(view) }

// FadeCamera applies a timed client camera fade.
func (p *Player) FadeCamera(fade camera.Fade) *session.Delivery { return p.session().FadeCamera(fade) }

// ClearCamera restores the player's normal camera.
func (p *Player) ClearCamera() *session.Delivery { return p.session().ClearCamera() }

// ClearTitle removes the current title without clearing the camera.
func (p *Player) ClearTitle() *session.Delivery { return p.session().ClearTitle() }
