package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type formImageState struct {
	mu         sync.Mutex
	generation uint64
	pending    int64
}

func (s *Session) formImagesEnabled() bool {
	return s.conf.FormImagesEnabled == nil || s.conf.FormImagesEnabled()
}

func (s *Session) cancelFormImages() {
	s.formImages.mu.Lock()
	s.formImages.generation++
	s.formImages.pending = 0
	s.formImages.mu.Unlock()
}

func (s *Session) scheduleFormImages(data []byte) {
	s.cancelFormImages()
	if !s.formImagesEnabled() || !containsFormURLImage(data) {
		return
	}
	s.formImages.mu.Lock()
	generation := s.formImages.generation
	s.formImages.mu.Unlock()
	go func() {
		if !s.formImageDelay(50 * time.Millisecond) {
			return
		}
		s.formImages.mu.Lock()
		if generation != s.formImages.generation || !s.formImagesEnabled() {
			s.formImages.mu.Unlock()
			return
		}
		timestamp := time.Now().UnixNano()
		s.formImages.pending = timestamp
		s.formImages.mu.Unlock()
		d := s.queueDelivery(&packet.NetworkStackLatency{Timestamp: timestamp, NeedsResponse: true})
		if d.Wait(context.Background()) == nil {
			s.formImageDelay(5 * time.Second)
		}
		s.formImages.mu.Lock()
		if s.formImages.pending == timestamp {
			s.formImages.pending = 0
		}
		s.formImages.mu.Unlock()
	}()
}

type FormImageAckHandler struct{}

func (*FormImageAckHandler) Handle(pk packet.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	timestamp := pk.(*packet.NetworkStackLatency).Timestamp
	s.formImages.mu.Lock()
	if timestamp == 0 || s.formImages.pending != timestamp {
		s.formImages.mu.Unlock()
		return nil
	}
	s.formImages.pending = 0
	generation := s.formImages.generation
	s.formImages.mu.Unlock()
	go s.refreshFormImages(generation)
	return nil
}

func (s *Session) refreshFormImages(generation uint64) {
	for range 5 {
		if !s.formImageDelay(500 * time.Millisecond) {
			return
		}
		active := false
		task := world.NewEntityRef[Controllable](s.ent).Do(func(_ *world.Tx, c Controllable) {
			s.formImages.mu.Lock()
			current := s.formImages.generation
			s.formImages.mu.Unlock()
			select {
			case <-s.closeBackground:
				return
			default:
			}
			if current != generation || !s.formImagesEnabled() {
				return
			}
			active = true
			s.SendExperience(c.ExperienceLevel(), c.ExperienceProgress())
		})
		select {
		case <-s.closeBackground:
			task.Cancel()
			return
		case <-task.Done():
		}
		if task.Err() != nil || !active {
			return
		}
	}
}

func (s *Session) formImageDelay(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.closeBackground:
		return false
	case <-timer.C:
		return true
	}
}

func containsFormURLImage(data []byte) bool {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	return findFormURLImage(value)
}
func findFormURLImage(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		if image, ok := value["image"].(map[string]any); ok {
			kind, _ := image["type"].(string)
			data, _ := image["data"].(string)
			if strings.EqualFold(kind, "url") && strings.TrimSpace(data) != "" {
				return true
			}
		}
		for _, child := range value {
			if findFormURLImage(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if findFormURLImage(child) {
				return true
			}
		}
	}
	return false
}
