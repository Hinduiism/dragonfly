package player

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PresentationName is the plain name published in player-list and actor packets.
// Name, UUID, XUID and authenticated connection data are never overridden.
func (p *Player) PresentationName() string {
	if p.presentationName != "" {
		return p.presentationName
	}
	return p.Name()
}

// SetPresentationName changes the packet presentation on the player's world
// owner. Empty text restores the account name. Authorization belongs to callers.
// Name tags are separate and continue to be controlled by SetNameTag.
func (p *Player) SetPresentationName(name string) error {
	if len(name) > 512 || !utf8.ValidString(name) || strings.ContainsAny(name, "§<>") || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
	}) {
		return errors.New("presentation name must be plain single-line text")
	}
	name = strings.TrimSpace(name)
	if p.presentationName == name {
		return nil
	}
	p.presentationName = name
	p.session().PublishPresentationName(p, p.PresentationName())
	return nil
}
