package session

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// Connection is a read-only reference to one session. It never resolves by
// account name or XUID and therefore cannot retarget a later login.
type Connection struct{ s *Session }

type ConnectionEndKind string

const (
	ConnectionEndTransport        ConnectionEndKind = "transport"
	ConnectionEndServerDisconnect ConnectionEndKind = "server_disconnect"
	ConnectionEndServerClose      ConnectionEndKind = "server_close"
)

// ConnectionEnd describes the first terminal event observed by this session.
type ConnectionEnd struct {
	Kind   ConnectionEndKind
	Reason string
	Err    error
}

func (s *Session) Connection() *Connection {
	if s == nil || s == Nop {
		return nil
	}
	return &Connection{s: s}
}
func (c *Connection) Done() <-chan struct{}            { return c.s.closeBackground }
func (c *Connection) Handle() *world.EntityHandle      { return c.s.ent }
func (c *Connection) IdentityData() login.IdentityData { return c.s.conn.IdentityData() }
func (c *Connection) ClientData() login.ClientData     { return c.s.conn.ClientData() }
func (c *Connection) RemoteAddr() net.Addr             { return c.s.conn.RemoteAddr() }
func (c *Connection) Latency() time.Duration           { return c.s.conn.Latency() }
func (c *Connection) ConnectionStats() (minecraft.ConnectionStats, bool) {
	if source, ok := c.s.conn.(minecraft.ConnectionStatsSource); ok {
		return source.ConnectionStats()
	}
	return minecraft.ConnectionStats{}, false
}
func (c *Connection) End() (ConnectionEnd, bool) {
	c.s.outputMu.Lock()
	defer c.s.outputMu.Unlock()
	return c.s.connectionEnd, c.s.outputClosed
}

func (s *Session) closeConnection(end ConnectionEnd) {
	s.connOnce.Do(func() {
		s.outputMu.Lock()
		s.outputClosed = true
		if s.disconnectReason != "" {
			end.Kind, end.Reason = ConnectionEndServerDisconnect, s.disconnectReason
		}
		s.connectionEnd = end
		close(s.closeBackground)
	draining:
		for {
			select {
			case pk := <-s.packets:
				if tracked, ok := pk.(deliveryPacket); ok {
					tracked.delivery.finish(net.ErrClosed)
				}
			default:
				break draining
			}
		}
		s.outputMu.Unlock()
		if s.conf.ConnectionEnded != nil {
			go s.conf.ConnectionEnded(s.Connection(), end)
		}
		_ = s.conn.Close()
	})
}
