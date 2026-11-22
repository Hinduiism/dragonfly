package session

import (
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type experienceDisplayPlayer struct{ Controllable }

func (experienceDisplayPlayer) ExperienceLevel() int        { return 0 }
func (experienceDisplayPlayer) ExperienceProgress() float64 { return 0 }

func TestExperienceCountdownSendsDisplayAndRetriesClear(t *testing.T) {
	s := &Session{packets: make(chan packet.Packet, 1)}
	p := experienceDisplayPlayer{}
	s.ViewExperienceCountdown(p, 10*time.Second, 10*time.Second)
	if len(s.packets) != 1 {
		t.Fatal("countdown did not send its initial update")
	}
	// A full queue should leave the clear pending for the next tick.
	s.ViewExperienceCountdown(p, 0, 0)
	if s.experienceCountdown.total == 0 {
		t.Fatal("clear was lost when the connection queue filled")
	}
	initial := (<-s.packets).(*packet.UpdateAttributes)
	if initial.Attributes[0].Value != 10 || initial.Attributes[1].Value < 0.99 {
		t.Fatalf("unexpected initial XP attributes: %+v", initial.Attributes)
	}
	s.experienceCountdown.nextUpdate = time.Time{}
	s.TickExperienceCountdown(p)
	if len(s.packets) != 1 || s.experienceCountdown.total != 0 {
		t.Fatal("countdown clear did not complete")
	}
	cleared := (<-s.packets).(*packet.UpdateAttributes)
	if cleared.Attributes[0].Value != 0 || cleared.Attributes[1].Value != 0 {
		t.Fatalf("unexpected cleared XP attributes: %+v", cleared.Attributes)
	}
}
