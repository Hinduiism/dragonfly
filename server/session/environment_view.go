package session

import (
	"math/rand/v2"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type environmentViewState struct {
	publicTime      int
	publicTimeKnown bool
	time            int
	timeOverridden  bool

	publicWeather      environmentWeather
	publicWeatherKnown bool
	weather            environmentWeather
	weatherOverridden  bool
}

type environmentWeather struct {
	raining bool
	thunder bool
}

// OverrideTime persistently replaces public world time for this session until
// ClearTimeOverride is called or the session changes worlds.
func (s *Session) OverrideTime(value int) {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	if s.environmentViews.timeOverridden && s.environmentViews.time == value {
		return
	}
	s.environmentViews.time = value
	s.environmentViews.timeOverridden = true
	s.writeEnvironmentTime(value)
}

// ClearTimeOverride resumes the latest public world time for this session.
func (s *Session) ClearTimeOverride() {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	if !s.environmentViews.timeOverridden {
		return
	}
	s.environmentViews.timeOverridden = false
	if s.environmentViews.publicTimeKnown {
		s.writeEnvironmentTime(s.environmentViews.publicTime)
	}
}

// OverrideWeather persistently replaces public world weather for this session
// until ClearWeatherOverride is called or the session changes worlds.
func (s *Session) OverrideWeather(raining, thunder bool) {
	if s == nil || s == Nop {
		return
	}
	weather := environmentWeather{raining: raining, thunder: thunder}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	if s.environmentViews.weatherOverridden && s.environmentViews.weather == weather {
		return
	}
	s.environmentViews.weather = weather
	s.environmentViews.weatherOverridden = true
	s.writeEnvironmentWeather(weather)
}

// ClearWeatherOverride resumes the latest public world weather for this
// session.
func (s *Session) ClearWeatherOverride() {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	if !s.environmentViews.weatherOverridden {
		return
	}
	s.environmentViews.weatherOverridden = false
	if s.environmentViews.publicWeatherKnown {
		s.writeEnvironmentWeather(s.environmentViews.publicWeather)
	}
}

func (s *Session) viewPublicTime(value int) {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	s.environmentViews.publicTime = value
	s.environmentViews.publicTimeKnown = true
	if s.environmentViews.timeOverridden {
		value = s.environmentViews.time
	}
	s.writeEnvironmentTime(value)
}

func (s *Session) viewPublicWeather(raining, thunder bool) {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	defer s.environmentViewsMu.Unlock()
	weather := environmentWeather{raining: raining, thunder: thunder}
	s.environmentViews.publicWeather = weather
	s.environmentViews.publicWeatherKnown = true
	if s.environmentViews.weatherOverridden {
		weather = s.environmentViews.weather
	}
	s.writeEnvironmentWeather(weather)
}

// clearEnvironmentViews drops overrides without sending restoration packets.
// World switches publish the destination world's values immediately after it.
func (s *Session) clearEnvironmentViews() {
	if s == nil || s == Nop {
		return
	}
	s.environmentViewsMu.Lock()
	s.environmentViews = environmentViewState{}
	s.environmentViewsMu.Unlock()
}

func (s *Session) writeEnvironmentTime(value int) {
	s.writePacket(&packet.SetTime{Time: int32(value)})
}

func (s *Session) writeEnvironmentWeather(weather environmentWeather) {
	rain := &packet.LevelEvent{EventType: packet.LevelEventStopRaining}
	if weather.raining {
		rain.EventType, rain.EventData = packet.LevelEventStartRaining, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(rain)

	thunder := &packet.LevelEvent{EventType: packet.LevelEventStopThunderstorm}
	if weather.thunder {
		thunder.EventType, thunder.EventData = packet.LevelEventStartThunderstorm, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(thunder)
}
