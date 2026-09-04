package player

import (
	"errors"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

var (
	// ErrVirtualContainerUnavailable is returned when a Player has no active network session.
	ErrVirtualContainerUnavailable = errors.New("virtual container is unavailable")
	// ErrInvalidVirtualContainer is returned when a virtual container configuration is invalid.
	ErrInvalidVirtualContainer = errors.New("invalid virtual container configuration")
)

// VirtualContainerTransaction describes one successful client request against an open virtual container.
type VirtualContainerTransaction struct {
	// ChangedSlots contains the unique virtual-container slots changed by the request.
	ChangedSlots []int
	// TransientEmpty reports whether all temporary cursor and crafting slots are empty after the request.
	TransientEmpty bool
}

// VirtualContainerConfig configures a private chest window shown only to one Player. Callbacks receive the current
// transaction and Player rather than the transaction-bound Player used to open the window. OnClose may receive nil
// values during detached teardown.
type VirtualContainerConfig struct {
	Inventory     *inventory.Inventory
	Title         string
	OnOpen        func(*world.Tx, *Player)
	OnTransaction func(*world.Tx, *Player, VirtualContainerTransaction)
	OnClose       func(*world.Tx, *Player)
}

// OpenVirtualChest opens a private 27-slot or 54-slot chest backed by the Inventory in config. The fake chest blocks
// used by the client are never added to the World.
func (p *Player) OpenVirtualChest(tx *world.Tx, config VirtualContainerConfig) error {
	if tx == nil || config.Inventory == nil {
		return ErrInvalidVirtualContainer
	}
	size := config.Inventory.Size()
	if size != 27 && size != 54 {
		return ErrInvalidVirtualContainer
	}
	s := p.session()
	if s == session.Nop {
		return ErrVirtualContainerUnavailable
	}

	direction := p.Rotation().Direction()
	pos := cube.PosFromVec3(p.Position()).Side(direction.Opposite().Face()).Side(direction.Opposite().Face())
	y := pos.Y() + 2
	r := tx.Range()
	if y < r.Min() {
		y = r.Min()
	}
	if y >= r.Max() {
		y = r.Max() - 1
	}
	pos[1] = y

	handle := p.H()
	return s.OpenVirtualChest(tx, pos, direction, session.VirtualContainerConfig{
		Inventory: config.Inventory,
		Title:     config.Title,
		MoveTransient: func(tx *world.Tx) {
			if current, ok := playerFromHandle(tx, handle); ok {
				current.MoveItemsToInventory()
			}
		},
		OnOpen: func(tx *world.Tx) {
			if config.OnOpen != nil {
				if current, ok := playerFromHandle(tx, handle); ok {
					config.OnOpen(tx, current)
				}
			}
		},
		OnTransaction: func(tx *world.Tx, event session.VirtualContainerTransaction) {
			if config.OnTransaction != nil {
				if current, ok := playerFromHandle(tx, handle); ok {
					config.OnTransaction(tx, current, VirtualContainerTransaction{
						ChangedSlots:   event.ChangedSlots,
						TransientEmpty: event.TransientEmpty,
					})
				}
			}
		},
		OnClose: func(tx *world.Tx) {
			if config.OnClose == nil {
				return
			}
			current, _ := playerFromHandle(tx, handle)
			config.OnClose(tx, current)
		},
	})
}

func playerFromHandle(tx *world.Tx, handle *world.EntityHandle) (*Player, bool) {
	if tx == nil || handle == nil {
		return nil, false
	}
	entity, ok := handle.Entity(tx)
	if !ok {
		return nil, false
	}
	p, ok := entity.(*Player)
	return p, ok
}

// CloseContainer closes the Player's current block or virtual container.
func (p *Player) CloseContainer(tx *world.Tx) {
	if tx != nil && p.session() != session.Nop {
		p.session().CloseContainer(tx)
	}
}
