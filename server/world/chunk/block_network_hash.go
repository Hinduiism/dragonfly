package chunk

// ConvertBlockNetworkHashesToRuntimeIDs converts block palette values from
// network hashes to registry runtime IDs. Unknown hashes remain unchanged.
func (chunk *Chunk) ConvertBlockNetworkHashesToRuntimeIDs() {
	if chunk == nil {
		return
	}
	for _, sub := range chunk.sub {
		if sub != nil {
			sub.ConvertBlockNetworkHashesToRuntimeIDs(chunk.br)
		}
	}
}

// ConvertBlockNetworkHashesToRuntimeIDs converts block palette values from
// network hashes to registry runtime IDs. Unknown hashes remain unchanged.
func (sub *SubChunk) ConvertBlockNetworkHashesToRuntimeIDs(br BlockRegistry) {
	if sub == nil || br == nil {
		return
	}
	for _, storage := range sub.storages {
		if storage == nil || storage.palette == nil {
			continue
		}
		storage.palette.Replace(func(hash uint32) uint32 {
			if runtimeID, ok := br.HashToRuntimeID(hash); ok {
				return runtimeID
			}
			return hash
		})
	}
}

// EncodeWithBlockNetworkHashes encodes a clone of chunk with block palette
// runtime IDs converted to network hashes. Unknown runtime IDs remain
// unchanged, and chunk itself is never mutated.
func EncodeWithBlockNetworkHashes(chunk *Chunk) SerialisedData {
	if chunk == nil {
		return SerialisedData{}
	}
	networkChunk := chunk.Clone()
	for _, sub := range networkChunk.sub {
		if sub != nil {
			sub.convertRuntimeIDsToBlockNetworkHashes(networkChunk.br)
		}
	}
	return Encode(networkChunk, NetworkEncoding)
}

func (sub *SubChunk) convertRuntimeIDsToBlockNetworkHashes(br BlockRegistry) {
	if sub == nil || br == nil {
		return
	}
	for _, storage := range sub.storages {
		if storage == nil || storage.palette == nil {
			continue
		}
		storage.palette.Replace(func(runtimeID uint32) uint32 {
			if hash, ok := br.RuntimeIDToHash(runtimeID); ok {
				return hash
			}
			return runtimeID
		})
	}
}
