package session

import (
	"reflect"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestVirtualChestIsPrivateAndDoesNotMutateWorld(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		pos := cube.Pos{8, 70, 8}
		pair := pos.Side(cube.East.Face())
		originals := map[cube.Pos]world.Block{
			pos:                    block.Dirt{},
			pos.Side(cube.FaceUp):  block.Stone{},
			pair:                   block.Grass{},
			pair.Side(cube.FaceUp): block.Sand{},
		}
		for p, b := range originals {
			tx.SetBlock(p, b, nil)
		}

		opener := virtualTestSession()
		other := virtualTestSession()
		container := inventory.New(54, nil)
		if err := opener.OpenVirtualChest(tx, pos, cube.North, VirtualContainerConfig{Inventory: container, Title: "Vault"}); err != nil {
			t.Fatal(err)
		}
		for p, want := range originals {
			if got := tx.Block(p); !sameBlock(got, want) {
				t.Fatalf("world block at %v = %T, want %T", p, got, want)
			}
		}
		if len(other.packets) != 0 {
			t.Fatalf("non-opener received %d virtual chest packets", len(other.packets))
		}
		if opener.openedWindow.Load() != container || !opener.containerOpened.Load() {
			t.Fatal("virtual inventory was not installed as the open window")
		}
		var opened, contents bool
		for len(opener.packets) != 0 {
			switch (<-opener.packets).(type) {
			case *packet.ContainerOpen:
				opened = true
			case *packet.InventoryContent:
				contents = true
			}
		}
		if !opened || !contents {
			t.Fatal("opener did not receive the container and inventory packets")
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestVirtualChestRoutesRequestsAndPublishesTransientState(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		s := virtualTestSession()
		container := inventory.New(54, nil)
		stack := item.NewStack(item.Diamond{}, 1)
		mustSetItem(t, container, 0, stack)

		var events []VirtualContainerTransaction
		if err := s.OpenVirtualChest(tx, cube.Pos{0, 70, 0}, cube.North, VirtualContainerConfig{
			Inventory: container,
			OnTransaction: func(callbackTx *world.Tx, event VirtualContainerTransaction) {
				if callbackTx != tx {
					t.Fatal("transaction callback received the wrong world transaction")
				}
				events = append(events, event)
			},
		}); err != nil {
			t.Fatal(err)
		}
		stack, _ = container.Item(0)

		h := newStackRequestHandler()
		if err := h.handleRequest(protocol.ItemStackRequest{
			RequestID: 1,
			Actions: []protocol.StackRequestAction{
				stackRequestTake(1, stackRequestSlot(protocol.ContainerLevelEntity, 0, stack), stackRequestSlot(protocol.ContainerCursor, 0, item.Stack{})),
			},
		}, s, tx, &stackRequestControllable{}); err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || !slices.Equal(events[0].ChangedSlots, []int{0}) || events[0].TransientEmpty {
			t.Fatalf("first event = %#v, want slot 0 with a non-empty transient inventory", events)
		}
		cursorStack, err := s.ui.Item(0)
		if err != nil {
			t.Fatal(err)
		}

		h.current = h.current.Add(1)
		if err := h.handleRequest(protocol.ItemStackRequest{
			RequestID: 2,
			Actions: []protocol.StackRequestAction{
				stackRequestTake(1, stackRequestSlot(protocol.ContainerCursor, 0, cursorStack), stackRequestSlot(protocol.ContainerInventory, 0, item.Stack{})),
			},
		}, s, tx, &stackRequestControllable{}); err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 || len(events[1].ChangedSlots) != 0 || !events[1].TransientEmpty {
			t.Fatalf("second event = %#v, want no virtual slots and an empty transient inventory", events)
		}
		assertStackEqual(t, s.inv, 0, stack)
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestVirtualChestRejectedRequestDoesNotPublish(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		s := virtualTestSession()
		container := inventory.New(27, nil)
		stack := item.NewStack(item.Diamond{}, 1)
		mustSetItem(t, container, 0, stack)
		container.Handle(cancellingInventoryHandler{takeSlot: 0, placeSlot: -1, dropSlot: -1})
		published := 0
		if err := s.OpenVirtualChest(tx, cube.Pos{0, 70, 0}, cube.North, VirtualContainerConfig{
			Inventory:     container,
			OnTransaction: func(*world.Tx, VirtualContainerTransaction) { published++ },
		}); err != nil {
			t.Fatal(err)
		}

		h := newStackRequestHandler()
		if err := h.handleRequest(protocol.ItemStackRequest{
			RequestID: 1,
			Actions: []protocol.StackRequestAction{
				stackRequestTake(1, stackRequestSlot(protocol.ContainerLevelEntity, 0, stack), stackRequestSlot(protocol.ContainerInventory, 0, item.Stack{})),
			},
		}, s, tx, &stackRequestControllable{}); err == nil {
			t.Fatal("request unexpectedly succeeded")
		}
		if published != 0 {
			t.Fatalf("rejected request published %d events", published)
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestVirtualChestCloseIsExactOnceAndMovesTransientItemsFirst(t *testing.T) {
	runtime := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = runtime.Close() })

	err := runtime.Do(func(tx *world.Tx) {
		s := virtualTestSession()
		closed, moved := 0, 0
		if err := s.ui.SetItem(0, item.NewStack(item.Diamond{}, 1)); err != nil {
			t.Fatal(err)
		}
		if err := s.OpenVirtualChest(tx, cube.Pos{0, 70, 0}, cube.North, VirtualContainerConfig{
			Inventory: inventory.New(54, nil),
			MoveTransient: func(*world.Tx) {
				moved++
				s.ui.Clear()
			},
			OnClose: func(callbackTx *world.Tx) {
				if callbackTx != tx {
					t.Fatal("close callback received the wrong world transaction")
				}
				closed++
				if !s.ui.Empty() {
					t.Fatal("close callback ran before transient items were moved")
				}
			},
		}); err != nil {
			t.Fatal(err)
		}
		s.CloseContainer(tx)
		s.CloseContainer(tx)
		if moved != 1 || closed != 1 {
			t.Fatalf("move/close callbacks = %d/%d, want 1/1", moved, closed)
		}
		if s.containerOpened.Load() || s.virtualContainer.Load() != nil {
			t.Fatal("virtual container state remained attached after close")
		}
	}).Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func virtualTestSession() *Session {
	s := &Session{
		inv:             inventory.New(36, nil),
		ui:              inventory.New(54, nil),
		offHand:         inventory.New(1, nil),
		packets:         make(chan packet.Packet, 64),
		closeBackground: make(chan struct{}),
		br:              world.DefaultBlockRegistry,
	}
	s.openedWindow.Store(inventory.New(1, nil))
	s.openedPos.Store(&cube.Pos{})
	return s
}

func sameBlock(a, b world.Block) bool {
	an, ap := a.EncodeBlock()
	bn, bp := b.EncodeBlock()
	return an == bn && reflect.DeepEqual(ap, bp)
}
