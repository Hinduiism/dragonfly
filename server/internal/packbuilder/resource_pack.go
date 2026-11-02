package packbuilder

import (
	_ "embed"
	"os"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/resource"
	"golang.org/x/mod/sumdb/dirhash"
)

//go:embed pack_icon.png
var packIcon []byte

// BuildResourcePack builds a resource pack based on custom features that have been registered to the server.
// It creates a UUID based on the hash of the directory so the client will only be prompted to download it
// once it is changed.
func BuildResourcePack(reg world.BlockRegistry, minimumGameVersion [3]int) (*resource.Pack, bool) {
	if minimumGameVersion == [3]int{} {
		minimumGameVersion = parseVersion(protocol.CurrentVersion)
	}
	dir, err := os.MkdirTemp("", "dragonfly_resource_pack-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	var assets int
	var lang []string

	itemCount, itemLang := buildItems(dir)
	assets += itemCount
	lang = append(lang, itemLang...)

	blockCount, blockLang := buildBlocks(reg, dir)
	assets += blockCount
	lang = append(lang, blockLang...)

	if assets > 0 {
		buildLanguageFile(dir, lang)
		if err := os.WriteFile(dir+"/pack_icon.png", packIcon, 0666); err != nil {
			panic(err)
		}
		// Include manifest semantics in the content identity. Changing only the
		// minimum engine must not reuse a cached pack with an older manifest.
		buildManifest(dir, uuid.Nil, uuid.Nil, minimumGameVersion)
		hash, err := dirhash.HashDir(dir, "", dirhash.Hash1)
		if err != nil {
			panic(err)
		}
		var header, module [16]byte
		copy(header[:], hash)
		copy(module[:], hash[16:])
		buildManifest(dir, header, module, minimumGameVersion)
		return resource.MustReadPath(dir), true
	}
	return nil, false
}
