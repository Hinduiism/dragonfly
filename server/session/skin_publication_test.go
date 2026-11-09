package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestPresentationRefreshResendsOwnSkinAfterPlayerList(t *testing.T) {
	l := new(sessionList)
	withPublicationSessionList(t, l)
	target, _ := newPublicationSession("Alex", 32, nil)
	actor := newPublicationActor(target, "Alex")
	setPublicationSessions(l, target)
	if !l.reconcilePlayerList(target, target) {
		t.Fatal("initial self player list failed")
	}
	_ = drainPublicationPackets(target)

	for _, appearance := range []struct{ name, id string }{{"Blue Fox", "nickname-skin"}, {"Alex", "original-skin"}} {
		value := skin.New(64, 64)
		value.FullID = appearance.id
		actor.appearance = value
		target.SetPlayerListSkin(value)
		// SetSkin publishes its packet before the name refresh replaces the
		// local player-list entry. Simulate that existing packet order.
		target.writePacket(&packet.PlayerSkin{UUID: actor.UUID(), Skin: skinToProtocol(value)})
		target.PublishPresentationName(actor, appearance.name)
		packets := drainPublicationPackets(target)
		lastList, lastSkin := -1, -1
		for index, pk := range packets {
			switch pk := pk.(type) {
			case *packet.PlayerList:
				if pk.Entries[0].ActionType == protocol.PlayerListActionAdd {
					lastList = index
				}
			case *packet.PlayerSkin:
				if pk.UUID != actor.UUID() || pk.Skin.FullID != appearance.id {
					t.Fatalf("wrong self skin: %#v", pk)
				}
				lastSkin = index
			case *packet.AddPlayer, *packet.RemoveActor:
				t.Fatalf("self refresh must not respawn the local actor: %T", pk)
			}
		}
		if lastList < 0 || lastSkin <= lastList {
			t.Fatalf("skin update must follow replacement player-list entry; list=%d skin=%d packets=%#v", lastList, lastSkin, packets)
		}
	}
}

func TestClassicSkinPacketHasExplicitAppearanceMetadata(t *testing.T) {
	for _, geometry := range []string{"geometry.humanoid.custom", "geometry.humanoid.customSlim"} {
		t.Run(geometry, func(t *testing.T) {
			value := skin.New(64, 64)
			value.FullID = "appearance-id"
			value.ModelConfig.Default = geometry
			encoded := skinToProtocol(value)
			if !encoded.Trusted || !encoded.OverrideAppearance || !encoded.PrimaryUser || encoded.PersonaSkin {
				t.Fatalf("generated skin lacks classic appearance flags: trusted=%t override=%t primary=%t persona=%t", encoded.Trusted, encoded.OverrideAppearance, encoded.PrimaryUser, encoded.PersonaSkin)
			}
			armSize := uint8(protocol.ArmSizeWide)
			if geometry == "geometry.humanoid.customSlim" {
				armSize = protocol.ArmSizeSlim
			}
			if encoded.ArmSize != armSize {
				t.Fatalf("arm size=%d, want %d", encoded.ArmSize, armSize)
			}
		})
	}
}
