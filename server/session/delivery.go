package session

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var (
	ErrOutputFull        = errors.New("session: output queue full")
	ErrDeliveryCancelled = errors.New("session: delivery cancelled")
)

// Delivery reports a write to this exact connection, not an acknowledgement
// from Minecraft. Wait must never be called on a world owner. Cancelling a
// pending delivery prevents its write; a write already in progress cannot be
// cancelled. Callbacks run on their own goroutines.
type Delivery struct {
	mu                sync.Mutex
	done              chan struct{}
	started, finished bool
	err               error
}

func newDelivery() *Delivery { return &Delivery{done: make(chan struct{})} }

func failedDelivery(err error) *Delivery {
	d := newDelivery()
	d.finish(err)
	return d
}

func (d *Delivery) Done() <-chan struct{} { return d.done }
func (d *Delivery) Err() error            { d.mu.Lock(); defer d.mu.Unlock(); return d.err }
func (d *Delivery) Wait(ctx context.Context) error {
	select {
	case <-d.done:
		return d.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *Delivery) OnDone(f func(error)) { go func() { <-d.done; f(d.Err()) }() }
func (d *Delivery) Cancel() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started || d.finished {
		return false
	}
	d.finished, d.err = true, ErrDeliveryCancelled
	close(d.done)
	return true
}
func (d *Delivery) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return false
	}
	d.started = true
	return true
}
func (d *Delivery) finish(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return
	}
	d.finished, d.err = true, err
	close(d.done)
}

// deliveryPacket is private to the session queue. It is unwrapped before any
// connection wrapper (including multiversion and anticheat) sees the packet.
type deliveryPacket struct {
	packet.Packet
	delivery *Delivery
}

func (s *Session) queueDelivery(pk packet.Packet) *Delivery {
	if s == nil || s == Nop {
		return failedDelivery(net.ErrClosed)
	}
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	if s.outputClosed {
		return failedDelivery(net.ErrClosed)
	}
	d := newDelivery()
	select {
	case s.packets <- deliveryPacket{pk, d}:
	default:
		d.finish(ErrOutputFull)
	}
	return d
}

func (s *Session) writeOutput() {
	for {
		select {
		case <-s.closeBackground:
			return
		case pk := <-s.packets:
			var d *Delivery
			if tracked, ok := pk.(deliveryPacket); ok {
				pk, d = tracked.Packet, tracked.delivery
				if !d.begin() {
					continue
				}
			}
			s.outputMu.Lock()
			closed := s.outputClosed
			s.outputMu.Unlock()
			if closed {
				if d != nil {
					d.finish(net.ErrClosed)
				}
				return
			}
			err := s.conn.WritePacket(pk)
			if d != nil {
				d.finish(err)
			}
			if err != nil {
				s.closeConnection(ConnectionEnd{Kind: ConnectionEndTransport, Err: err})
				return
			}
		}
	}
}
