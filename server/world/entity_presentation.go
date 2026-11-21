package world

// EntityPresentation controls whether an actor is sent to a viewer and which name tag it uses.
type EntityPresentation struct {
	Hidden          bool
	NameTag         string
	OverrideNameTag bool
}

// ViewerPresentedEntity supplies session-local presentation for a non-player actor.
// The method runs on the actor's world owner and must return without blocking.
type ViewerPresentedEntity interface {
	PresentationFor(viewer *EntityHandle) EntityPresentation
}
