package player

import "testing"

func TestPresentationNameDoesNotChangeAccountName(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		account, id, xuid := p.Name(), p.UUID(), p.XUID()
		if err := p.SetPresentationName("Blue Fox"); err != nil {
			t.Fatal(err)
		}
		if p.PresentationName() != "Blue Fox" || p.Name() != account || p.UUID() != id || p.XUID() != xuid {
			t.Fatal("presentation changed authenticated identity")
		}
		for _, name := range []string{"bad\nname", "§aName", "<red>Name", "a\u200db"} {
			if err := p.SetPresentationName(name); err == nil {
				t.Fatalf("accepted %q", name)
			}
		}
		if p.PresentationName() != "Blue Fox" {
			t.Fatal("rejected name changed override")
		}
		if err := p.SetPresentationName(""); err != nil || p.PresentationName() != account {
			t.Fatal("reset", err)
		}
	})
}
