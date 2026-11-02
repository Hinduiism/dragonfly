package server

import (
	"log/slog"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft"
)

type testProtocol struct {
	minecraft.Protocol
	id int32
}

func (p testProtocol) ID() int32 { return p.id }

func TestListenerProtocolsArePerConfig(t *testing.T) {
	a := Config{Log: slog.Default(), Allower: allower{}, AcceptedProtocols: []minecraft.Protocol{testProtocol{id: 844}}}
	b := Config{Log: slog.Default(), Allower: allower{}, AcceptedProtocols: []minecraft.Protocol{testProtocol{id: 860}}}
	x, y := listenerConfig(a), listenerConfig(b)
	if x.AcceptedProtocols[0].ID() != 844 || y.AcceptedProtocols[0].ID() != 860 {
		t.Fatal("listener protocols leaked between server configurations")
	}
	x.AcceptedProtocols[0] = testProtocol{id: 2193}
	if a.AcceptedProtocols[0].ID() != 844 {
		t.Fatal("listener shares the caller's protocol slice")
	}
	native := listenerConfig(Config{Log: slog.Default(), Allower: allower{}})
	if len(native.AcceptedProtocols) != 0 {
		t.Fatal("native-only default registers additional protocols")
	}
}
