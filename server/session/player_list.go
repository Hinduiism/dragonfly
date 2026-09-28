package session

import (
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type playerListState struct {
	listed      bool
	initialized bool
	revision    uint64
}

func (l *sessionList) reconcilePlayerList(target, viewer *Session) bool {
	if target == nil || viewer == nil || target.ent == nil || viewer.ent == nil {
		return false
	}
	_, listed, revision := viewer.playerVisibility(target.ent)
	return l.applyPlayerListDecision(target, viewer, listed, revision)
}

func (l *sessionList) applyPlayerListDecision(target, viewer *Session, listed bool, revision uint64) bool {
	viewer.publicationMu.Lock()
	if !l.containsPair(target, viewer) {
		viewer.publicationMu.Unlock()
		return false
	}
	state := viewer.playerList[target.ent]
	if state.initialized && revision < state.revision {
		viewer.publicationMu.Unlock()
		return true
	}
	if state.initialized && revision == state.revision && listed != state.listed {
		viewer.publicationMu.Unlock()
		return false
	}
	runtimeID := viewer.ensurePlayerRuntimeID(target)
	if state.initialized && state.listed == listed {
		state.revision = revision
		viewer.playerList[target.ent] = state
		viewer.publicationMu.Unlock()
		return true
	}

	var pk packet.Packet
	if listed {
		pk = &packet.PlayerList{Entries: []protocol.PlayerListEntry{target.playerListEntry(runtimeID)}}
	} else if state.initialized {
		pk = playerListRemove(target.ent.UUID())
	}
	if pk != nil && !viewer.writePublicationPacket(pk) {
		viewer.publicationMu.Unlock()
		viewer.CloseConnection()
		return false
	}
	viewer.playerList[target.ent] = playerListState{listed: listed, initialized: true, revision: revision}
	viewer.publicationMu.Unlock()
	return true
}

func (l *sessionList) removePlayerListEntry(target, viewer *Session) {
	if target == nil || viewer == nil || target.ent == nil {
		return
	}
	viewer.publicationMu.Lock()
	state := viewer.playerList[target.ent]
	queued := true
	if state.initialized && state.listed {
		queued = viewer.writePublicationPacket(playerListRemove(target.ent.UUID()))
	}
	delete(viewer.playerList, target.ent)
	viewer.entityMutex.Lock()
	if runtimeID, ok := viewer.entityRuntimeIDs[target.ent]; ok {
		delete(viewer.entities, runtimeID)
		delete(viewer.entityRuntimeIDs, target.ent)
	}
	viewer.entityMutex.Unlock()
	viewer.publicationMu.Unlock()
	if !queued {
		viewer.CloseConnection()
	}
}

func (s *Session) playerVisibility(target *world.EntityHandle) (entityVisible, listed bool, revision uint64) {
	if target == s.ent {
		return true, true, 0
	}
	if s.conf.PlayerVisibility == nil {
		return true, true, 0
	}
	return s.conf.PlayerVisibility(s.ent, target)
}

func (s *Session) ensurePlayerRuntimeID(target *Session) uint64 {
	s.entityMutex.Lock()
	defer s.entityMutex.Unlock()
	if runtimeID, ok := s.entityRuntimeIDs[target.ent]; ok {
		return runtimeID
	}
	runtimeID := uint64(selfEntityRuntimeID)
	if target != s {
		s.currentEntityRuntimeID++
		runtimeID = s.currentEntityRuntimeID
	}
	s.entityRuntimeIDs[target.ent] = runtimeID
	s.entities[runtimeID] = target.ent
	return runtimeID
}

func (s *Session) playerListEntry(runtimeID uint64) protocol.PlayerListEntry {
	current := s.playerListSkin.Load()
	var playerSkin skin.Skin
	if current != nil {
		playerSkin = *current
	}
	return protocol.PlayerListEntry{
		ActionType:     protocol.PlayerListActionAdd,
		UUID:           s.ent.UUID(),
		EntityUniqueID: int64(runtimeID),
		Username:       s.conn.IdentityData().DisplayName,
		XUID:           s.conn.IdentityData().XUID,
		BuildPlatform:  int32(protocol.DeviceUnknown),
		Skin:           skinToProtocol(playerSkin),
	}
}

func playerListRemove(id uuid.UUID) *packet.PlayerList {
	return &packet.PlayerList{Entries: []protocol.PlayerListEntry{{ActionType: protocol.PlayerListActionRemove, UUID: id}}}
}

// RefreshPlayerListEntry re-evaluates one exact connected target for this
// viewer. It returns false if either session is unavailable or publication
// could not be queued.
func (s *Session) RefreshPlayerListEntry(target *world.EntityHandle) bool {
	if s == nil || s == Nop || target == nil {
		return false
	}
	targetSession, ok := sessions.LookupHandle(target)
	return ok && sessions.reconcilePlayerList(targetSession, s)
}

// RefreshPlayerList re-evaluates every connected target for this viewer.
func (s *Session) RefreshPlayerList() {
	if s == nil || s == Nop {
		return
	}
	for _, target := range sessions.snapshot() {
		if !sessions.reconcilePlayerList(target, s) {
			return
		}
	}
}

// skinToProtocol converts a skin to its protocol representation.
func skinToProtocol(s skin.Skin) protocol.Skin {
	var animations []protocol.SkinAnimation
	for _, animation := range s.Animations {
		protocolAnim := protocol.SkinAnimation{
			ImageWidth:  uint32(animation.Bounds().Max.X),
			ImageHeight: uint32(animation.Bounds().Max.Y),
			ImageData:   animation.Pix,
			FrameCount:  float32(animation.FrameCount),
		}
		switch animation.Type() {
		case skin.AnimationHead:
			protocolAnim.AnimationType = protocol.SkinAnimationHead
		case skin.AnimationBody32x32:
			protocolAnim.AnimationType = protocol.SkinAnimationBody32x32
		case skin.AnimationBody128x128:
			protocolAnim.AnimationType = protocol.SkinAnimationBody128x128
		}
		protocolAnim.ExpressionType = uint32(animation.AnimationExpression)
		animations = append(animations, protocolAnim)
	}

	fullID := s.FullID
	if fullID == "" {
		fullID = uuid.New().String()
	}
	model := s.Model
	if len(model) == 0 {
		model = []byte("{}")
	}
	return protocol.Skin{
		PlayFabID:                 s.PlayFabID,
		SkinID:                    uuid.New().String(),
		SkinResourcePatch:         s.ModelConfig.Encode(),
		SkinImageWidth:            uint32(s.Bounds().Max.X),
		SkinImageHeight:           uint32(s.Bounds().Max.Y),
		SkinData:                  s.Pix,
		CapeImageWidth:            uint32(s.Cape.Bounds().Max.X),
		CapeImageHeight:           uint32(s.Cape.Bounds().Max.Y),
		CapeData:                  s.Cape.Pix,
		SkinGeometry:              model,
		PersonaSkin:               s.Persona,
		CapeID:                    uuid.New().String(),
		FullID:                    fullID,
		Animations:                animations,
		Trusted:                   true,
		OverrideAppearance:        true,
		GeometryDataEngineVersion: []byte(protocol.CurrentVersion),
	}
}
