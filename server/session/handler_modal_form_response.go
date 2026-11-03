package session

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"sync"
	"sync/atomic"
)

// ModalFormResponseHandler handles the ModalFormResponse packet.
type ModalFormResponseHandler struct {
	mu                           sync.Mutex
	forms                        map[uint32]form.Form
	currentID                    atomic.Uint32
	settings                     form.Form
	settingsID, activeSettingsID uint32
}

// Handle ...
func (h *ModalFormResponseHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.ModalFormResponse)

	h.mu.Lock()
	f, ok := h.forms[pk.FormID]
	delete(h.forms, pk.FormID)
	if pk.FormID&(1<<31) != 0 {
		if h.settings == nil || pk.FormID != h.activeSettingsID {
			h.mu.Unlock()
			return nil
		}
		f, ok, h.settings = h.settings, true, nil
	}
	h.mu.Unlock()

	resp, exists := pk.ResponseData.Value()
	if !ok && !exists {
		// Sometimes the client seems to send a second response with no data, which would cause the player to be kicked
		// by the server. This should patch that.
		return nil
	}
	if !exists || len(resp) == 0 {
		// The form was cancelled: The cross in the top right corner was clicked.
		resp = nil
	}
	if !ok {
		return fmt.Errorf("no form with ID %v currently opened", pk.FormID)
	}
	s.cancelFormImagesFor(pk.FormID)
	if err := f.SubmitJSON(resp, c, tx); err != nil {
		if pk.FormID&(1<<31) != 0 {
			s.conf.Log.Error("submit server settings", "error", err)
			return nil
		}
		return fmt.Errorf("error submitting form data: %w", err)
	}
	return nil
}
