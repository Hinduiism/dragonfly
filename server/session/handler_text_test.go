package session

import (
	"fmt"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type textControllable struct {
	Controllable
	messages []string
}

func (c *textControllable) Chat(message ...any) {
	c.messages = append(c.messages, fmt.Sprint(message...))
}

func TestChatAcceptsConnectionAccountAndPublishedNickname(t *testing.T) {
	s, conn := newPublicationSession("Alex", 32, nil)
	s.presentationName.Store(&presentationSnapshot{name: "Blue Fox", revision: 1})
	c := new(textControllable)
	for _, name := range []string{"Alex", "Blue Fox"} {
		pk := &packet.Text{TextType: packet.TextTypeChat, SourceName: name, XUID: conn.identity.XUID, Message: "message from " + name}
		if err := (TextHandler{}).Handle(pk, s, nil, c); err != nil {
			t.Fatalf("chat from %q rejected: %v", name, err)
		}
	}
	if len(c.messages) != 2 || conn.IdentityData().DisplayName != "Alex" {
		t.Fatal("chat did not use the same authenticated connection")
	}
	// Reset retires the nickname as an accepted sender label.
	s.presentationName.Store(&presentationSnapshot{name: "Alex", revision: 2})
	if err := (TextHandler{}).Handle(&packet.Text{TextType: packet.TextTypeChat, SourceName: "Blue Fox", XUID: conn.identity.XUID}, s, nil, c); err == nil {
		t.Fatal("retired nickname remained accepted")
	}
}

func TestNickChatStillRejectsSpoofedIdentityAndWrongTextType(t *testing.T) {
	s, conn := newPublicationSession("Alex", 32, nil)
	s.presentationName.Store(&presentationSnapshot{name: "Blue Fox", revision: 1})
	for _, test := range []struct {
		name string
		pk   packet.Text
	}{
		{"other account", packet.Text{TextType: packet.TextTypeChat, SourceName: "Someone Else", XUID: conn.identity.XUID}},
		{"other XUID with nick", packet.Text{TextType: packet.TextTypeChat, SourceName: "Blue Fox", XUID: "another-account"}},
		{"other XUID with account", packet.Text{TextType: packet.TextTypeChat, SourceName: "Alex", XUID: "another-account"}},
		{"wrong type", packet.Text{TextType: packet.TextTypeRaw, SourceName: "Blue Fox", XUID: conn.identity.XUID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := new(textControllable)
			if err := (TextHandler{}).Handle(&test.pk, s, nil, c); err == nil || len(c.messages) != 0 {
				t.Fatal("invalid chat reached controllable", err)
			}
		})
	}
}
