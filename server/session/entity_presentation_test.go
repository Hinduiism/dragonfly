package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type presentedNPC struct {
	visibilityEntity
	views map[*world.EntityHandle]world.EntityPresentation
}

func (a *presentedNPC) PresentationFor(viewer *world.EntityHandle) world.EntityPresentation {
	if view, ok := a.views[viewer]; ok {
		return view
	}
	return world.EntityPresentation{Hidden: true}
}

func TestPrivateActorPublishesOnlyToEligibleViewer(t *testing.T) {
	first, _ := newPublicationSession("First", 12, nil)
	second, _ := newPublicationSession("Second", 12, nil)
	actor := &presentedNPC{
		visibilityEntity: visibilityEntity{handle: world.EntitySpawnOpts{}.New(visibilityEntityType{}, visibilityEntityConfig{})},
		views:            make(map[*world.EntityHandle]world.EntityPresentation),
	}
	first.ViewEntity(actor)
	second.ViewEntity(actor)
	if len(first.packets) != 0 || len(second.packets) != 0 {
		t.Fatal("hidden actor was sent before eligibility was granted")
	}
	actor.views[first.ent] = world.EntityPresentation{NameTag: "Quest ready", OverrideNameTag: true}
	first.RefreshEntityPresentation(actor)
	packets := drainPublicationPackets(first)
	if len(packets) != 1 || len(second.packets) != 0 {
		t.Fatalf("reveal sent %d packets to first player and %d to second", len(packets), len(second.packets))
	}
	add, ok := packets[0].(*packet.AddActor)
	if !ok || add.EntityMetadata[protocol.EntityDataKeyName] != "Quest ready" {
		t.Fatalf("unexpected reveal: %#v", packets[0])
	}
	actor.views[first.ent] = world.EntityPresentation{NameTag: "Quest claimed", OverrideNameTag: true}
	first.RefreshEntityPresentation(actor)
	packets = drainPublicationPackets(first)
	if len(packets) != 1 {
		t.Fatalf("rename sent %d packets", len(packets))
	}
	state, ok := packets[0].(*packet.SetActorData)
	if !ok || state.EntityRuntimeID != add.EntityRuntimeID || state.EntityMetadata[protocol.EntityDataKeyName] != "Quest claimed" {
		t.Fatalf("unexpected rename: %#v", packets[0])
	}
	delete(actor.views, first.ent)
	first.RefreshEntityPresentation(actor)
	first.ViewEntityAnimation(actor, world.NewEntityAnimation("animation.example.greet"))
	packets = drainPublicationPackets(first)
	if len(packets) != 1 {
		t.Fatalf("hide sent %d packets", len(packets))
	}
	if remove, ok := packets[0].(*packet.RemoveActor); !ok || remove.EntityUniqueID != add.EntityUniqueID {
		t.Fatalf("unexpected hide: %#v", packets[0])
	}
}
