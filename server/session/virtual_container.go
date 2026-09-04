package session

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var errInvalidVirtualContainer = errors.New("virtual container inventory must contain 27 or 54 slots")

// virtualContainerOpenDelay gives the client time to register the client-only
// chest blocks before it receives the window open packet.
const virtualContainerOpenDelay = 500 * time.Millisecond

// VirtualContainerTransaction describes one successful request processed while a virtual container is open.
type VirtualContainerTransaction struct {
	ChangedSlots   []int
	TransientEmpty bool
}

// VirtualContainerConfig configures a private chest window. Callbacks run synchronously on the current owner with
// its transaction. The transaction passed to MoveTransient and OnClose may be nil during detached teardown.
type VirtualContainerConfig struct {
	Inventory     *inventory.Inventory
	Title         string
	MoveTransient func(*world.Tx)
	OnOpen        func(*world.Tx)
	OnTransaction func(*world.Tx, VirtualContainerTransaction)
	OnClose       func(*world.Tx)
}

type virtualContainer struct {
	inventory     *inventory.Inventory
	blocks        []virtualContainerBlock
	pos           cube.Pos
	windowID      byte
	onOpen        func(*world.Tx)
	onTransaction func(*world.Tx, VirtualContainerTransaction)
	onClose       func(*world.Tx)
	moveTransient func(*world.Tx)

	lifecycleMu sync.Mutex
	openTask    *world.Task
	opened      bool
	closed      bool
}

type virtualContainerBlock struct {
	pos      cube.Pos
	original world.Block
}

// OpenVirtualChest opens a client-only chest at pos. The chest faces facing and is paired to its right when the
// supplied inventory has 54 slots.
func (s *Session) OpenVirtualChest(tx *world.Tx, pos cube.Pos, facing cube.Direction, config VirtualContainerConfig) error {
	if tx == nil || config.Inventory == nil || config.Inventory.Size() != 27 && config.Inventory.Size() != 54 {
		return errInvalidVirtualContainer
	}
	s.closeCurrentContainer(tx, false)

	positions := []cube.Pos{pos, pos.Side(cube.FaceUp)}
	var pair cube.Pos
	if config.Inventory.Size() == 54 {
		pair = pos.Side(facing.RotateLeft().Face())
		positions = append(positions, pair, pair.Side(cube.FaceUp))
	}
	blocks := make([]virtualContainerBlock, 0, len(positions))
	for _, blockPos := range positions {
		blocks = append(blocks, virtualContainerBlock{pos: blockPos, original: tx.Block(blockPos)})
	}

	state := &virtualContainer{
		inventory:     config.Inventory,
		blocks:        blocks,
		pos:           pos,
		onOpen:        config.OnOpen,
		onTransaction: config.OnTransaction,
		onClose:       config.OnClose,
		moveTransient: config.MoveTransient,
	}
	s.virtualContainer.Store(state)

	chest := block.NewChest()
	chest.Facing = facing
	chest.CustomName = config.Title
	s.ViewBlockUpdate(pos, chest, 0)
	s.ViewBlockUpdate(pos.Side(cube.FaceUp), block.Air{}, 0)
	if config.Inventory.Size() == 54 {
		s.ViewBlockUpdate(pair, chest, 0)
		s.ViewBlockUpdate(pair.Side(cube.FaceUp), block.Air{}, 0)
		s.writeVirtualChestPair(pos, pair, config.Title)
		s.writeVirtualChestPair(pair, pos, config.Title)
	}

	state.windowID = s.nextWindowID()
	s.containerOpened.Store(true)
	s.openedWindow.Store(config.Inventory)
	s.openedPos.Store(&pos)
	s.openedContainerID.Store(protocol.ContainerTypeContainer)
	task := tx.World().DoAfter(virtualContainerOpenDelay, func(openTx *world.Tx) {
		s.finishVirtualChestOpen(openTx, state)
	})
	state.lifecycleMu.Lock()
	if state.closed {
		task.Cancel()
	} else {
		state.openTask = task
	}
	state.lifecycleMu.Unlock()
	return nil
}

func (s *Session) finishVirtualChestOpen(tx *world.Tx, state *virtualContainer) {
	if tx == nil || state == nil {
		return
	}
	state.lifecycleMu.Lock()
	if state.closed || state.opened || s.virtualContainer.Load() != state ||
		s.openedWindow.Load() != state.inventory || !s.containerOpened.Load() {
		state.lifecycleMu.Unlock()
		return
	}
	state.opened = true
	s.writePacket(&packet.ContainerOpen{
		WindowID:                state.windowID,
		ContainerType:           protocol.ContainerTypeContainer,
		ContainerPosition:       protocol.BlockPos{int32(state.pos.X()), int32(state.pos.Y()), int32(state.pos.Z())},
		ContainerEntityUniqueID: -1,
	})
	s.sendInv(state.inventory, uint32(state.windowID))
	state.lifecycleMu.Unlock()
	if state.onOpen != nil {
		state.onOpen(tx)
	}
}

// CloseContainer closes the current container, if one is open.
func (s *Session) CloseContainer(tx *world.Tx) {
	s.closeCurrentContainer(tx, false)
}

func (s *Session) writeVirtualChestPair(pos, pair cube.Pos, title string) {
	data := map[string]any{
		"id":    "Chest",
		"x":     int32(pos.X()),
		"y":     int32(pos.Y()),
		"z":     int32(pos.Z()),
		"pairx": int32(pair.X()),
		"pairz": int32(pair.Z()),
	}
	if title != "" {
		data["CustomName"] = title
	}
	s.writePacket(&packet.BlockActorData{
		Position: protocol.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())},
		NBTData:  data,
	})
}

func (s *Session) closeVirtualContainer(tx *world.Tx, clientRequested bool) bool {
	state := s.virtualContainer.Swap(nil)
	if state == nil {
		return false
	}
	state.lifecycleMu.Lock()
	state.closed = true
	opened := state.opened
	if state.openTask != nil {
		state.openTask.Cancel()
	}
	state.lifecycleMu.Unlock()
	if state.moveTransient != nil {
		state.moveTransient(tx)
	}
	s.closeWindow(clientRequested || !opened)
	if tx != nil {
		for _, entry := range state.blocks {
			s.ViewBlockUpdate(entry.pos, entry.original, 0)
		}
	}
	if state.onClose != nil {
		state.onClose(tx)
	}
	return true
}

func (s *Session) virtualContainerRequest(changes map[byte]map[byte]changeInfo, tx *world.Tx) (*virtualContainer, VirtualContainerTransaction, bool) {
	state := s.virtualContainer.Load()
	if state == nil || s.openedWindow.Load() != state.inventory {
		return nil, VirtualContainerTransaction{}, false
	}
	seen := make(map[int]struct{})
	for containerID, slots := range changes {
		inv, ok := s.invByID(int32(containerID), tx)
		if !ok || inv != state.inventory {
			continue
		}
		for slot := range slots {
			seen[int(slot)] = struct{}{}
		}
	}
	changed := make([]int, 0, len(seen))
	for slot := range seen {
		changed = append(changed, slot)
	}
	sort.Ints(changed)
	return state, VirtualContainerTransaction{ChangedSlots: changed, TransientEmpty: s.ui.Empty()}, true
}

func (s *Session) publishVirtualContainerRequest(tx *world.Tx, state *virtualContainer, event VirtualContainerTransaction) {
	if state == nil || state.onTransaction == nil || s.virtualContainer.Load() != state {
		return
	}
	state.onTransaction(tx, event)
}
