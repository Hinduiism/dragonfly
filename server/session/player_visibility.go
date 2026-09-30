package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type playerActorState struct {
	streamed    bool
	visible     bool
	allowed     bool
	initialized bool
	revision    uint64
}

type playerActor interface {
	world.Entity
	Name() string
	UUID() uuid.UUID
	GameMode() world.GameMode
}

type playerActorPublication struct {
	add  *packet.AddPlayer
	skin *packet.PlayerSkin
}

// RefreshPlayerVisibility re-evaluates an exact connected player's actor and
// player-list publication for this viewer without changing explicit entity
// visibility markers.
func (s *Session) RefreshPlayerVisibility(target world.Entity) bool {
	if s == nil || target == nil {
		return false
	}
	actor, valid := target.(playerActor)
	if !valid {
		return false
	}
	targetSession, connected := sessions.LookupHandle(target.H())
	return connected && s.refreshConnectedPlayer(actor, targetSession)
}

func (s *Session) viewConnectedPlayer(actor playerActor, target *Session) bool {
	entityVisible, listed, revision := s.playerVisibility(target.ent)
	var publication playerActorPublication
	if entityVisible {
		publication = s.snapshotPlayerActor(actor, target)
	}
	ok, _ := s.reconcileConnectedPlayer(actor, target, publication, entityVisible, listed, revision, true)
	return ok
}

func (s *Session) refreshConnectedPlayer(actor playerActor, target *Session) bool {
	entityVisible, listed, revision := s.playerVisibility(target.ent)
	var publication playerActorPublication
	if entityVisible {
		publication = s.snapshotPlayerActor(actor, target)
	}
	ok, revealed := s.reconcileConnectedPlayer(actor, target, publication, entityVisible, listed, revision, false)
	if ok && revealed {
		s.ViewEntityItems(actor)
		s.ViewEntityArmour(actor)
	}
	return ok

}

func (s *Session) reconcileConnectedPlayer(actor playerActor, target *Session, publication playerActorPublication, entityVisible, listed bool, revision uint64, streamed bool) (bool, bool) {
	if entityVisible && listed {
		if !sessions.applyPlayerListDecision(target, s, true, revision) {
			return false, false
		}
		return s.applyPlayerActorDecision(actor, target, publication, true, revision, streamed)
	}
	ok, revealed := s.applyPlayerActorDecision(actor, target, publication, entityVisible, revision, streamed)
	if !ok {
		return false, false
	}
	if !sessions.applyPlayerListDecision(target, s, listed, revision) {
		return false, false
	}
	return true, revealed
}

// hideConnectedPlayer removes an exact connected player's actor from this
// viewer. It returns true when e belongs to a connected player, even if the
// required removal packet could not be queued and the viewer was closed.
func (s *Session) hideConnectedPlayer(e world.Entity, retire bool) bool {
	if e == nil {
		return false
	}
	target, connected := sessions.LookupHandle(e.H())
	if !connected {
		return false
	}

	s.publicationMu.Lock()
	state, tracked := s.playerActors[target.ent]
	if retire && tracked {
		state.streamed = false
	}
	queued := true
	if tracked && state.visible {
		runtimeID := s.ensurePlayerRuntimeID(target)
		queued = s.writePublicationPacket(&packet.RemoveActor{EntityUniqueID: int64(runtimeID)})
		state.visible = false
	}
	if tracked {
		s.playerActors[target.ent] = state
	}
	s.publicationMu.Unlock()
	if !queued {
		s.CloseConnection()
	}
	return true
}

func (s *Session) applyPlayerActorDecision(actor playerActor, target *Session, publication playerActorPublication, visible bool, revision uint64, streamed bool) (bool, bool) {
	s.publicationMu.Lock()
	if !target.registered.Load() || !s.registered.Load() {
		s.publicationMu.Unlock()
		return false, false
	}
	state := s.playerActors[target.ent]
	if state.initialized && revision < state.revision {
		s.publicationMu.Unlock()
		return true, false
	}
	if state.initialized && revision == state.revision && visible != state.allowed {
		s.publicationMu.Unlock()
		return false, false
	}
	if streamed {
		state.streamed = true
	}
	desired := visible && state.streamed && !s.entityExplicitlyHidden(actor)
	if state.initialized && state.visible == desired {
		state.allowed, state.revision = visible, revision
		s.playerActors[target.ent] = state
		s.publicationMu.Unlock()
		return true, false
	}

	runtimeID := s.ensurePlayerRuntimeID(target)
	var packets []packet.Packet
	if desired {
		listState := s.playerList[target.ent]
		if !listState.listed {
			packets = append(packets, &packet.PlayerList{Entries: []protocol.PlayerListEntry{target.playerListEntry(runtimeID)}})
		}
		publication.add.EntityRuntimeID = runtimeID
		publication.add.AbilityData.EntityUniqueID = int64(runtimeID)
		packets = append(packets, publication.add, publication.skin)
		if !listState.listed {
			packets = append(packets, playerListRemove(target.ent.UUID()))
		}
	} else if state.visible {
		packets = append(packets, &packet.RemoveActor{EntityUniqueID: int64(runtimeID)})
	}
	if !s.writePublicationPackets(packets) {
		s.publicationMu.Unlock()
		s.CloseConnection()
		return false, false
	}
	revealed := desired && !state.visible
	state.visible, state.allowed, state.initialized, state.revision = desired, visible, true, revision
	s.playerActors[target.ent] = state
	s.publicationMu.Unlock()
	return true, revealed
}

func (s *Session) snapshotPlayerActor(actor playerActor, target *Session) playerActorPublication {
	yaw, pitch := actor.Rotation().Elem()
	return playerActorPublication{
		add: &packet.AddPlayer{
			EntityMetadata: s.entityMetadata(actor),
			GameType:       gameTypeFromMode(actor.GameMode()),
			HeadYaw:        float32(yaw),
			Pitch:          float32(pitch),
			Position:       vec64To32(actor.Position()),
			UUID:           actor.UUID(),
			Username:       actor.Name(),
			Yaw:            float32(yaw),
			BuildPlatform:  int32(protocol.DeviceUnknown),
			AbilityData: protocol.AbilityData{
				Layers: []protocol.AbilityLayer{{
					Type:      protocol.AbilityLayerTypeBase,
					Abilities: protocol.AbilityCount - 1,
				}},
			},
		},
		skin: &packet.PlayerSkin{UUID: actor.UUID(), Skin: target.playerListProtocolSkin()},
	}
}

func (s *Session) playerActorPublished(handle *world.EntityHandle) (connected, visible bool) {
	if handle == s.ent {
		return true, true
	}
	s.publicationMu.RLock()
	_, connected = s.playerList[handle]
	state := s.playerActors[handle]
	s.publicationMu.RUnlock()
	return connected, !connected || state.visible
}

func (s *Session) writePublicationPackets(packets []packet.Packet) bool {
	for _, pk := range packets {
		if !s.writePublicationPacket(pk) {
			return false
		}
	}
	return true
}
