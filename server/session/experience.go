package session

import "time"

type experienceCountdown struct {
	end, nextUpdate time.Time
	total           time.Duration
}

const experienceCountdownInterval = 100 * time.Millisecond

// ViewExperienceCountdown starts or clears a client-side XP display override.
// Call it on the controllable's world owner.
func (s *Session) ViewExperienceCountdown(c Controllable, total, remaining time.Duration) {
	if s == Nop {
		return
	}
	remaining = min(remaining, total)
	if remaining <= 0 {
		if s.experienceCountdown.total == 0 {
			return
		}
		s.experienceCountdown.end = time.Time{}
		s.experienceCountdown.nextUpdate = time.Time{}
	} else {
		now := time.Now()
		s.experienceCountdown = experienceCountdown{
			end: now.Add(remaining), total: total,
		}
	}
	s.TickExperienceCountdown(c)
}

// TickExperienceCountdown sends an update for active countdowns at most every
// 100 milliseconds, with a final update at expiry.
func (s *Session) TickExperienceCountdown(c Controllable) {
	timer := &s.experienceCountdown
	if timer.total == 0 {
		return
	}
	now := time.Now()
	if now.Before(timer.nextUpdate) {
		return
	}
	remaining := timer.end.Sub(now)
	timer.nextUpdate = now.Add(experienceCountdownInterval)
	if remaining > 0 && remaining < experienceCountdownInterval {
		timer.nextUpdate = timer.end
	}
	// HUD updates can wait for the next tick if the connection is congested.
	update := s.experiencePacket(c.ExperienceLevel(), c.ExperienceProgress())
	select {
	case s.packets <- update:
		if remaining <= 0 {
			*timer = experienceCountdown{}
		}
	case <-s.closeBackground:
		*timer = experienceCountdown{}
	default:
	}
}

func experienceRewardsEnabled(c Controllable) bool {
	xp, ok := c.(interface{ ExperienceEnabled() bool })
	return !ok || xp.ExperienceEnabled()
}
