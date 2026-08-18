package session

import (
	"sync"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestTimeOverrideSurvivesPublicRefreshAndRestoresLatestValue(t *testing.T) {
	s := newEnvironmentViewTestSession()
	s.ViewTime(6000)
	assertTimePacket(t, s, 6000)

	s.OverrideTime(18000)
	assertTimePacket(t, s, 18000)
	s.OverrideTime(18000)
	assertNoEnvironmentPacket(t, s)

	s.ViewTime(7000)
	assertTimePacket(t, s, 18000)
	s.ClearTimeOverride()
	assertTimePacket(t, s, 7000)
	s.ClearTimeOverride()
	assertNoEnvironmentPacket(t, s)
}

func TestWeatherOverrideSurvivesPublicRefreshAndRestoresLatestValue(t *testing.T) {
	s := newEnvironmentViewTestSession()
	s.ViewWeather(false, false)
	assertWeatherPackets(t, s, false, false)

	s.OverrideWeather(true, true)
	assertWeatherPackets(t, s, true, true)
	s.OverrideWeather(true, true)
	assertNoEnvironmentPacket(t, s)

	s.ViewWeather(true, false)
	assertWeatherPackets(t, s, true, true)
	s.ClearWeatherOverride()
	assertWeatherPackets(t, s, true, false)
	s.ClearWeatherOverride()
	assertNoEnvironmentPacket(t, s)
}

func TestClearEnvironmentViewsDropsOverridesAcrossWorldBoundary(t *testing.T) {
	s := newEnvironmentViewTestSession()
	s.ViewTime(6000)
	assertTimePacket(t, s, 6000)
	s.ViewWeather(false, false)
	assertWeatherPackets(t, s, false, false)
	s.OverrideTime(18000)
	assertTimePacket(t, s, 18000)
	s.OverrideWeather(true, true)
	assertWeatherPackets(t, s, true, true)

	s.clearEnvironmentViews()
	assertNoEnvironmentPacket(t, s)
	s.ViewTime(9000)
	assertTimePacket(t, s, 9000)
	s.ViewWeather(false, false)
	assertWeatherPackets(t, s, false, false)
}

func TestEnvironmentViewsAreRaceSafe(t *testing.T) {
	s := newEnvironmentViewTestSession()
	var workers sync.WaitGroup
	for index := range 32 {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			s.ViewTime(index)
			s.OverrideTime(18000 + index)
			s.ViewWeather(index%2 == 0, false)
			s.OverrideWeather(true, index%2 == 0)
			s.ClearTimeOverride()
			s.ClearWeatherOverride()
		}(index)
	}
	workers.Wait()
}

func newEnvironmentViewTestSession() *Session {
	return &Session{
		packets:         make(chan packet.Packet, 1024),
		closeBackground: make(chan struct{}),
	}
}

func packetOf[T packet.Packet](t *testing.T, s *Session) T {
	t.Helper()
	select {
	case pk := <-s.packets:
		got, ok := pk.(T)
		if !ok {
			var zero T
			t.Fatalf("packet type: got %T, want %T", pk, zero)
		}
		return got
	default:
		var zero T
		t.Fatalf("no packet queued, want %T", zero)
		return zero
	}
}

func assertNoEnvironmentPacket(t *testing.T, s *Session) {
	t.Helper()
	select {
	case pk := <-s.packets:
		t.Fatalf("unexpected packet %T", pk)
	default:
	}
}

func assertTimePacket(t *testing.T, session *Session, want int32) {
	t.Helper()
	got := packetOf[*packet.SetTime](t, session)
	if got.Time != want {
		t.Fatalf("SetTime.Time = %d, want %d", got.Time, want)
	}
}

func assertWeatherPackets(t *testing.T, session *Session, raining, thunder bool) {
	t.Helper()
	rain := packetOf[*packet.LevelEvent](t, session)
	thunderstorm := packetOf[*packet.LevelEvent](t, session)
	wantRain := int32(packet.LevelEventStopRaining)
	if raining {
		wantRain = int32(packet.LevelEventStartRaining)
	}
	wantThunder := int32(packet.LevelEventStopThunderstorm)
	if thunder {
		wantThunder = int32(packet.LevelEventStartThunderstorm)
	}
	if rain.EventType != wantRain || thunderstorm.EventType != wantThunder {
		t.Fatalf("weather events = %d/%d, want %d/%d", rain.EventType, thunderstorm.EventType, wantRain, wantThunder)
	}
}
