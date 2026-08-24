package world

// TickSubsystem identifies an independently configurable part of a World's
// tick cycle.
type TickSubsystem uint16

const (
	// TickRandomBlocks controls random block ticks.
	TickRandomBlocks TickSubsystem = 1 << iota
	// TickBlockEntities controls block entity ticks.
	TickBlockEntities
	// TickScheduledBlocks controls scheduled block updates.
	TickScheduledBlocks
	// TickNeighbourUpdates controls neighbour block updates.
	TickNeighbourUpdates
	// TickRedstone controls redstone evaluation and transient redstone state.
	TickRedstone
	// TickLightning controls lightning strike attempts during thunderstorms.
	TickLightning
	// TickSleep controls sleep countdowns and day advancement.
	TickSleep
	// TickNonPlayerEntities controls TickerEntity calls for non-player entities.
	TickNonPlayerEntities
)

// TickAllSubsystems contains every known TickSubsystem bit.
const TickAllSubsystems = TickRandomBlocks |
	TickBlockEntities |
	TickScheduledBlocks |
	TickNeighbourUpdates |
	TickRedstone |
	TickLightning |
	TickSleep |
	TickNonPlayerEntities

// TickPolicy controls which World tick subsystems are disabled. The zero value
// leaves every subsystem enabled.
type TickPolicy struct {
	Disabled TickSubsystem
}

// Enabled reports whether subsystem is enabled by the policy.
func (policy TickPolicy) Enabled(subsystem TickSubsystem) bool {
	return policy.Disabled&subsystem == 0
}
