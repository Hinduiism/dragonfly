package player

import (
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/session"
)

// SendServerSettings publishes a form in Bedrock's server-settings screen.
// Dragonfly tracks the exact connection and submits its response on the owner.
func (p *Player) SendServerSettings(f form.Form) *session.Delivery {
	return p.session().SendServerSettings(f)
}
