package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TextHandler handles the Text packet.
type TextHandler struct{}

// Handle ...
func (TextHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.Text)

	if pk.TextType != packet.TextTypeChat {
		return fmt.Errorf("TextType should always be Chat (%v), but got %v", packet.TextTypeChat, pk.TextType)
	}
	// Bedrock echoes the server-published player-list name as SourceName after
	// a nickname change. Other client versions keep sending the login name.
	// Accept only these two names for this connection; neither selects the
	// account that speaks. Chat is always dispatched through its controllable.
	if pk.SourceName != s.conn.IdentityData().DisplayName && pk.SourceName != s.presentation().name {
		return fmt.Errorf("SourceName must match the account or presentation name")
	}
	if pk.XUID != s.conn.IdentityData().XUID {
		return fmt.Errorf("XUID must be equal to player's XUID")
	}
	c.Chat(pk.Message)
	return nil
}
