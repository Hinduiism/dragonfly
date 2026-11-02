package packbuilder

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"testing"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/resource"
)

type packTestItem struct{}

func (packTestItem) EncodeItem() (string, int16) { return "packtest:icon", 0 }
func (packTestItem) Name() string                { return "Pack test icon" }
func (packTestItem) Texture() image.Image        { return image.NewNRGBA(image.Rect(0, 0, 1, 1)) }
func (packTestItem) Category() category.Category { return category.Items() }

func TestGeneratedPackEngineVersionAndCacheIdentity(t *testing.T) {
	world.RegisterItem(packTestItem{})
	world.DefaultBlockRegistry.Finalize()
	native, ok := BuildResourcePack(world.DefaultBlockRegistry, [3]int{})
	if !ok {
		t.Fatal("custom item pack was not generated")
	}
	legacy, ok := BuildResourcePack(world.DefaultBlockRegistry, [3]int{1, 21, 111})
	if !ok {
		t.Fatal("legacy custom item pack was not generated")
	}
	manifest, nativeFiles := readPack(t, native)
	if manifest.Header.MinimumGameVersion != parseVersion(protocol.CurrentVersion) {
		t.Fatalf("zero configuration changed native minimum: %v", manifest.Header.MinimumGameVersion)
	}
	manifest, legacyFiles := readPack(t, legacy)
	if manifest.Header.MinimumGameVersion != [3]int{1, 21, 111} || manifest.Header.Version != [3]int{0, 0, 2} {
		t.Fatalf("legacy manifest was not updated: %+v", manifest.Header)
	}
	if native.UUID() == legacy.UUID() {
		t.Fatal("different minimum engine versions reuse one cached pack identity")
	}
	if len(nativeFiles) != len(legacyFiles) {
		t.Fatal("content was lost while changing the manifest")
	}
	for name, body := range nativeFiles {
		if name != "manifest.json" && !bytes.Equal(body, legacyFiles[name]) {
			t.Fatalf("generated asset changed: %s", name)
		}
	}
	repeated, _ := BuildResourcePack(world.DefaultBlockRegistry, [3]int{1, 21, 111})
	if repeated.UUID() != legacy.UUID() || repeated.Version() != legacy.Version() {
		t.Fatal("unchanged generated content has an unstable cache identity")
	}
}

func readPack(t *testing.T, pack *resource.Pack) (resource.Manifest, map[string][]byte) {
	t.Helper()
	body := make([]byte, pack.Len())
	if _, err := pack.ReadAt(body, 0); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		reader, err := archive.Open(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		var content bytes.Buffer
		_, err = content.ReadFrom(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = content.Bytes()
	}
	var manifest resource.Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest, files
}
