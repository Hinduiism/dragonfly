package player

// ViewTime persistently shows time instead of the current world's public time
// until ViewWorldTime is called. The visible clock remains fixed across world changes.
func (p *Player) ViewTime(time int) {
	if p == nil {
		return
	}
	p.session().OverrideTime(time)
}

// ViewWorldTime resumes the current world's public time for p.
func (p *Player) ViewWorldTime() {
	if p == nil {
		return
	}
	p.session().ClearTimeOverride()
}

// ViewWeather persistently shows weather instead of the current world's public
// weather until ViewWorldWeather is called, including across world changes.
func (p *Player) ViewWeather(raining, thunder bool) {
	if p == nil {
		return
	}
	p.session().OverrideWeather(raining, thunder)
}

// ViewWorldWeather resumes the current world's public weather for p.
func (p *Player) ViewWorldWeather() {
	if p == nil {
		return
	}
	p.session().ClearWeatherOverride()
}
