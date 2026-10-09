package player

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestSetHealthValidatesStateClampsAndSendsAttributes(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.health.AddHealth(-15)
		if err := p.SetHealth(8); err != nil || p.Health() != 8 {
			t.Fatalf("low restore = health:%v error:%v", p.Health(), err)
		}
		assertAttribute(t, conn, "minecraft:health", 8)

		if err := p.SetHealth(200); err != nil || p.Health() != p.MaxHealth() {
			t.Fatalf("high restore = health:%v max:%v error:%v", p.Health(), p.MaxHealth(), err)
		}
		assertAttribute(t, conn, "minecraft:health", float32(p.MaxHealth()))

		before := p.Health()
		if err := p.SetHealth(0); err == nil || p.Health() != before {
			t.Fatalf("zero restore = health:%v error:%v", p.Health(), err)
		}
		if err := p.SetHealth(-1); err == nil || p.Health() != before {
			t.Fatalf("negative restore = health:%v error:%v", p.Health(), err)
		}
		if err := p.SetHealth(mgl64.Epsilon); err == nil || p.Health() != before {
			t.Fatalf("epsilon restore = health:%v error:%v", p.Health(), err)
		}

		p.health.SetHealth(0)
		if err := p.SetHealth(10); err == nil || !p.Dead() {
			t.Fatalf("dead restore = health:%v error:%v", p.Health(), err)
		}
		p.health.SetHealth(p.MaxHealth())
		removed := p.tx.RemoveEntity(p)
		_ = removed.Close()
		if err := p.SetHealth(10); err == nil {
			t.Fatal("closed player health was restored")
		}
	})
}

func TestResetHungerRestoresAllStateAndSendsAttributes(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.hunger.foodLevel = 2
		p.hunger.saturationLevel = 1
		p.hunger.exhaustionLevel = 3
		p.hunger.foodTick = 77
		p.ResetHunger()
		if p.hunger.foodLevel != 20 || p.hunger.saturationLevel != 5 || p.hunger.exhaustionLevel != 0 || p.hunger.foodTick != 1 {
			t.Fatalf("hunger = food:%d saturation:%v exhaustion:%v tick:%d", p.hunger.foodLevel, p.hunger.saturationLevel, p.hunger.exhaustionLevel, p.hunger.foodTick)
		}
		assertAttribute(t, conn, "minecraft:player.hunger", 20)
		assertAttribute(t, conn, "minecraft:player.saturation", 5)
		assertAttribute(t, conn, "minecraft:player.exhaustion", 0)
	})
}

func withStateTestPlayer(t *testing.T, test func(*Player, *stateConn)) {
	t.Helper()
	conn := newStateConn()
	sess := (session.Config{Log: slog.New(slog.DiscardHandler)}).New(conn)
	t.Cleanup(sess.CloseConnection)
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })
	handle := world.EntitySpawnOpts{}.New(Type, Config{Session: sess, Health: 20, MaxHealth: 20, GameMode: world.GameModeSurvival})
	if err := runtime.Do(func(tx *world.Tx) {
		added := tx.AddEntity(handle)
		p, ok := added.(*Player)
		if !ok {
			t.Fatalf("added entity = %T, want *Player", added)
		}
		defer func() {
			p.health.SetHealth(p.MaxHealth())
			p.s = nil
		}()
		test(p, conn)
	}).Wait(t.Context()); err != nil {
		t.Fatalf("world transaction error = %v", err)
	}
}

func assertAttribute(t *testing.T, conn *stateConn, name string, want float32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, pk := range conn.snapshot() {
			update, ok := pk.(*packet.UpdateAttributes)
			if !ok {
				continue
			}
			for _, attribute := range update.Attributes {
				if attribute.Name == name && attribute.Value == want {
					return
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("attribute %q with value %v was not sent", name, want)
}

type stateConn struct {
	mu      sync.Mutex
	packets []packet.Packet
	id      login.IdentityData
}

func newStateConn() *stateConn {
	id := uuid.New()
	return &stateConn{id: login.IdentityData{Identity: id.String(), DisplayName: "state-test", XUID: "1"}}
}

func (conn *stateConn) snapshot() []packet.Packet {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return append([]packet.Packet(nil), conn.packets...)
}

func (*stateConn) Close() error                          { return nil }
func (conn *stateConn) IdentityData() login.IdentityData { return conn.id }
func (*stateConn) ClientData() login.ClientData          { return login.ClientData{} }
func (*stateConn) ClientCacheEnabled() bool              { return false }
func (*stateConn) ChunkRadius() int                      { return 8 }
func (*stateConn) Latency() time.Duration                { return 0 }
func (*stateConn) Flush() error                          { return nil }
func (*stateConn) RemoteAddr() net.Addr                  { return &net.UDPAddr{} }
func (*stateConn) ReadPacket() (packet.Packet, error)    { return nil, io.EOF }
func (conn *stateConn) WritePacket(pk packet.Packet) error {
	conn.mu.Lock()
	conn.packets = append(conn.packets, pk)
	conn.mu.Unlock()
	return nil
}
func (*stateConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }
