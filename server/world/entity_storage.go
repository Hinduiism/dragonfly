package world

// EntityStorageMode controls whether entities are loaded from and written to a
// World's Provider. Runtime entity behaviour is unaffected.
type EntityStorageMode uint8

const (
	// EntityStoragePersistent loads and stores entities through the World's
	// Provider. This is the default mode.
	EntityStoragePersistent EntityStorageMode = iota
	// EntityStorageTransient ignores provider entities when loading chunks and
	// omits runtime entities when saving chunks.
	EntityStorageTransient
)
