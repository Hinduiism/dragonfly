package session

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var ErrServerSettingsOpen = errors.New("session: server settings already open")

// SendServerSettings publishes a native settings form using a reserved ID range
// on this session. A failed write releases only this form's reservation.
func (s *Session) SendServerSettings(f form.Form) *Delivery {
	if s == Nop {
		return failedDelivery(errors.New("session: player offline"))
	}
	data, err := f.MarshalJSON()
	if err != nil {
		return failedDelivery(fmt.Errorf("encode server settings: %w", err))
	}
	h := s.handlers[packet.IDModalFormResponse].(*ModalFormResponseHandler)
	h.mu.Lock()
	if h.settings != nil {
		h.mu.Unlock()
		return failedDelivery(ErrServerSettingsOpen)
	}
	h.settingsID++
	id := h.settingsID | 1<<31
	h.settings, h.activeSettingsID = f, id
	h.mu.Unlock()
	d := s.queueDelivery(&packet.ServerSettingsResponse{FormID: id, FormData: data})
	d.OnDone(func(err error) {
		if err != nil {
			h.mu.Lock()
			if h.activeSettingsID == id {
				h.settings = nil
			}
			h.mu.Unlock()
		}
	})
	return d
}

type ServerSettingsRequestHandler struct{}

func (*ServerSettingsRequestHandler) Handle(_ packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	if s.conf.ServerSettings == nil {
		return nil
	}
	h := s.handlers[packet.IDModalFormResponse].(*ModalFormResponseHandler)
	h.mu.Lock()
	open := h.settings != nil
	h.mu.Unlock()
	if open {
		return nil
	}
	if f := s.conf.ServerSettings(tx, c); f != nil {
		s.SendServerSettings(f).OnDone(func(err error) {
			if err != nil {
				s.conf.Log.Debug("send server settings", "error", err)
			}
		})
	}
	return nil
}
