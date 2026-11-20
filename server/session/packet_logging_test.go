package session

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestUnhandledPacketFormattingRequiresDebugLogging(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		debug bool
	}{
		{name: "info", level: slog.LevelInfo},
		{name: "debug", level: slog.LevelDebug, debug: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			log := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: test.level}))
			s := &Session{conf: Config{Log: log}}
			pk := &unhandledLoggingPacket{}
			if err := s.handlePacket(pk, nil, nil); err != nil {
				t.Fatal(err)
			}
			if pk.formatted != test.debug {
				t.Fatalf("packet formatting called = %t, want %t", pk.formatted, test.debug)
			}
			if test.debug && !strings.Contains(output.String(), "data=sample") {
				t.Fatalf("debug log missing packet data: %s", output.String())
			}
		})
	}
}

type unhandledLoggingPacket struct{ formatted bool }

func (*unhandledLoggingPacket) ID() uint32          { return 0 }
func (*unhandledLoggingPacket) Marshal(protocol.IO) {}

func (pk *unhandledLoggingPacket) String() string {
	pk.formatted = true
	return "&sample"
}
