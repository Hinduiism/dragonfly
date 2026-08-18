package player

import "testing"

func TestEnvironmentViewMethodsAcceptUnavailablePlayer(t *testing.T) {
	var p *Player
	p.ViewTime(18000)
	p.ViewWeather(true, true)
	p.ViewWorldTime()
	p.ViewWorldWeather()

	p = &Player{playerData: &playerData{}}
	p.ViewTime(18000)
	p.ViewWeather(true, true)
	p.ViewWorldTime()
	p.ViewWorldWeather()
}
