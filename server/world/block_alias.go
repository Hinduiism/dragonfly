package world

import "fmt"

// RegisterBlockAlias makes a native state behave and serialize as replacement in this registry only.
// Disk loads, world writes and client packets all resolve the same state. Call before Finalize.
// The replacement must be a registered state and must not itself be aliased.
func (br *BasicBlockRegistry) RegisterBlockAlias(state BlockState, replacement Block) {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.finalized {
		panic("BlockRegistry.RegisterBlockAlias called on finalized BlockRegistry")
	}
	name, properties := replacement.EncodeBlock()
	source := stateHash{name: state.Name, properties: hashProperties(state.Properties)}
	target := stateHash{name: name, properties: hashProperties(properties)}
	if state.Name == "minecraft:air" {
		panic("the air state cannot be aliased")
	}
	if _, ok := br.stateRuntimeIDs[source]; !ok {
		panic(fmt.Sprintf("block alias source is not registered: %s", state.Name))
	}
	if _, ok := br.stateRuntimeIDs[target]; !ok {
		panic(fmt.Sprintf("block alias replacement is not registered: %s", name))
	}
	if source == target {
		panic("block alias cannot reference itself")
	}
	if br.aliases == nil {
		br.aliases = make(map[stateHash]stateHash)
	}
	br.aliases[source] = target
}

// UnregisterBlockAlias restores a state's native behavior in a mutable registry.
func (br *BasicBlockRegistry) UnregisterBlockAlias(state BlockState) {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.finalized {
		panic("BlockRegistry.UnregisterBlockAlias called on finalized BlockRegistry")
	}
	delete(br.aliases, stateHash{name: state.Name, properties: hashProperties(state.Properties)})
}

func (br *BasicBlockRegistry) applyAliases() {
	for _, target := range br.aliases {
		if _, chained := br.aliases[target]; chained {
			panic("block alias replacement must not itself be aliased")
		}
	}
	br.runtimeAliases = make(map[uint32]uint32, len(br.aliases))
	for source, target := range br.aliases {
		from, to := br.stateRuntimeIDs[source], br.stateRuntimeIDs[target]
		br.runtimeAliases[from] = to
		br.stateRuntimeIDs[source] = to
		br.blockInfos[from] = br.blockInfos[to]
		br.networkhashToRids[br.ridsToNetworkhash[from]] = to
		br.ridsToNetworkhash[from] = br.ridsToNetworkhash[to]
	}
}

func (br *BasicBlockRegistry) canonicalRuntimeID(rid uint32) uint32 {
	if replacement, ok := br.runtimeAliases[rid]; ok {
		return replacement
	}
	return rid
}

// CanonicalRuntimeID normalizes an ID supplied by a generator or a structure to this registry's state.
func (br *BasicBlockRegistry) CanonicalRuntimeID(rid uint32) uint32 {
	if !br.finalized {
		panic("BlockRegistry.CanonicalRuntimeID called on non finalized BlockRegistry")
	}
	return br.canonicalRuntimeID(rid)
}
