package session

import (
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Modern Bedrock uses the player-list skin for listed actors. Reassert that
// entry after the immediate actor/name publication has been processed, rather
// than relying only on PlayerSkin. Keep at most one short timer per target.
const playerListSkinRefreshDelay = 100 * time.Millisecond

func (s *Session) queuePlayerListSkinRefresh(registry *sessionList) {
	if s == nil || s == Nop || !s.registered.Load() || !s.skinRefreshPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(playerListSkinRefreshDelay, func() {
		current := s.playerListSkin.Load()
		if current != nil && s.registered.Load() {
			for _, viewer := range registry.snapshot() {
				s.reassertPlayerListSkin(viewer, current)
			}
		}
		s.skinRefreshPending.Store(false)
		// A concurrent appearance change may have coalesced into this timer
		// after its snapshot was taken. Do not lose that newer update.
		if current != s.playerListSkin.Load() {
			s.queuePlayerListSkinRefresh(registry)
		}
	})
}

func (s *Session) reassertPlayerListSkin(viewer *Session, current *skin.Skin) {
	_, listed, revision := viewer.playerVisibility(s.ent)
	if !listed {
		return
	}
	viewer.publicationMu.Lock()
	state := viewer.playerList[s.ent]
	presentation := s.presentation()
	if !s.registered.Load() || !viewer.registered.Load() || s.playerListSkin.Load() != current || !state.listed || state.revision != revision || state.presentationRevision != presentation.revision {
		viewer.publicationMu.Unlock()
		return
	}
	entry := s.playerListEntryWithPresentation(viewer.ensurePlayerRuntimeID(s), presentation)
	// No removal or actor respawn: ADD updates an existing listed player's
	// cached appearance. All state checks and queuing share the publication lock.
	queued := viewer.writePublicationPacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{entry}})
	viewer.publicationMu.Unlock()
	if !queued {
		viewer.CloseConnection()
	}
}
