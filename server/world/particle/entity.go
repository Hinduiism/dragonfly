package particle

import "image/color"

// HugeExplosion is a particle shown when TNT or a creeper explodes.
type HugeExplosion struct{ particle }

// Critical is a particle shown when an entity is hit critically.
type Critical struct {
	particle

	// Scale controls the size of the particle. A value of 0 uses the default
	// scale of 2.
	Scale int
}

// Heart is a heart particle shown around entities in love mode.
type Heart struct {
	particle

	// Scale controls the size of the particle.
	Scale int
}

// SonicBoom is the particle burst produced by a warden's sonic boom.
type SonicBoom struct{ particle }

// WindExplosion is the particle burst produced by a wind charge.
type WindExplosion struct{ particle }

// Sparkler is the coloured particle emitted by an active sparkler.
type Sparkler struct {
	particle

	// Colour is the RGB colour of the sparkler. The alpha channel is ignored.
	Colour color.RGBA
}

// Totem is the particle burst produced when a totem activates.
type Totem struct{ particle }

// EndermanTeleport is a particle that shows up when an enderman teleports.
type EndermanTeleport struct{ particle }

// SnowballPoof is a particle shown when a snowball collides with something.
type SnowballPoof struct{ particle }

// EggSmash is a particle shown when an egg smashes on something.
type EggSmash struct{ particle }

// Splash is a particle that shows up when a splash potion is splashed.
type Splash struct {
	particle

	// Colour is the colour that should be splashed.
	Colour color.RGBA
}

// Effect is a particle that shows up around an entity when it has effects on.
type Effect struct {
	particle

	// Colour is the colour of the particle.
	Colour color.RGBA
}

// EntityFlame is a particle shown when an entity is set on fire.
type EntityFlame struct{ particle }
