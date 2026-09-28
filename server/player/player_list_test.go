package player

import "testing"

func TestDetachedPlayerListRefreshIsSafe(t *testing.T) {
	p := &Player{playerData: new(playerData)}
	if p.RefreshPlayerListEntry(nil) {
		t.Fatal("detached player refreshed a nil target")
	}
	p.RefreshPlayerList()
}
