package session

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type cancellingInventoryHandler struct {
	inventory.NopHandler
	takeSlot  int
	placeSlot int
	dropSlot  int
}

func (h cancellingInventoryHandler) HandleTake(ctx *inventory.Context, slot int, _ item.Stack) {
	if slot == h.takeSlot {
		ctx.Cancel()
	}
}

func (h cancellingInventoryHandler) HandlePlace(ctx *inventory.Context, slot int, _ item.Stack) {
	if slot == h.placeSlot {
		ctx.Cancel()
	}
}

func (h cancellingInventoryHandler) HandleDrop(ctx *inventory.Context, slot int, _ item.Stack) {
	if slot == h.dropSlot {
		ctx.Cancel()
	}
}

type stackRequestControllable struct {
	Controllable
	gameMode world.GameMode
	dropped  int
}

func (c *stackRequestControllable) GameMode() world.GameMode { return c.gameMode }

func (c *stackRequestControllable) Drop(stack item.Stack) int {
	c.dropped += stack.Count()
	return stack.Count()
}

func TestTransferHonoursSourceTakeCancellation(t *testing.T) {
	s, opened, playerInv := stackRequestSession()
	source := item.NewStack(item.Diamond{}, 4)
	mustSetItem(t, opened, 0, source)
	opened.Handle(cancellingInventoryHandler{takeSlot: 0, placeSlot: -1, dropSlot: -1})

	h := newStackRequestHandler()
	err := h.handleTransfer(stackRequestSlot(protocol.ContainerLevelEntity, 0, source), stackRequestSlot(protocol.ContainerInventory, 0, item.Stack{}), 2, s, nil, &stackRequestControllable{})
	if err == nil {
		t.Fatal("handleTransfer() succeeded after source take was cancelled")
	}
	assertStackEqual(t, opened, 0, source)
	assertStackEqual(t, playerInv, 0, item.Stack{})
}

func TestSwapHonoursEveryCancellationStage(t *testing.T) {
	tests := []struct {
		name          string
		sourceHandler inventory.Handler
		destHandler   inventory.Handler
	}{
		{name: "source take", sourceHandler: cancellingInventoryHandler{takeSlot: 0, placeSlot: -1, dropSlot: -1}},
		{name: "source place", sourceHandler: cancellingInventoryHandler{takeSlot: -1, placeSlot: 0, dropSlot: -1}},
		{name: "destination take", destHandler: cancellingInventoryHandler{takeSlot: 0, placeSlot: -1, dropSlot: -1}},
		{name: "destination place", destHandler: cancellingInventoryHandler{takeSlot: -1, placeSlot: 0, dropSlot: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, opened, playerInv := stackRequestSession()
			source := item.NewStack(item.Diamond{}, 1)
			destination := item.NewStack(item.Stick{}, 1)
			mustSetItem(t, opened, 0, source)
			mustSetItem(t, playerInv, 0, destination)
			if test.sourceHandler != nil {
				opened.Handle(test.sourceHandler)
			}
			if test.destHandler != nil {
				playerInv.Handle(test.destHandler)
			}

			h := newStackRequestHandler()
			err := h.handleSwap(&protocol.SwapStackRequestAction{
				Source:      stackRequestSlot(protocol.ContainerLevelEntity, 0, source),
				Destination: stackRequestSlot(protocol.ContainerInventory, 0, destination),
			}, s, nil, &stackRequestControllable{})
			if err == nil {
				t.Fatal("handleSwap() succeeded after an inventory handler cancelled it")
			}
			assertStackEqual(t, opened, 0, source)
			assertStackEqual(t, playerInv, 0, destination)
		})
	}
}

func TestDestroyHonoursSourceTakeCancellation(t *testing.T) {
	s, opened, _ := stackRequestSession()
	source := item.NewStack(item.Diamond{}, 3)
	mustSetItem(t, opened, 0, source)
	opened.Handle(cancellingInventoryHandler{takeSlot: 0, placeSlot: -1, dropSlot: -1})

	h := newStackRequestHandler()
	err := h.handleDestroy(&protocol.DestroyStackRequestAction{
		Count:  2,
		Source: stackRequestSlot(protocol.ContainerLevelEntity, 0, source),
	}, s, nil, &stackRequestControllable{gameMode: world.GameModeCreative})
	if err == nil {
		t.Fatal("handleDestroy() succeeded after source take was cancelled")
	}
	assertStackEqual(t, opened, 0, source)
}

func TestDropHonoursSourceDropCancellation(t *testing.T) {
	s, opened, _ := stackRequestSession()
	source := item.NewStack(item.Diamond{}, 3)
	mustSetItem(t, opened, 0, source)
	opened.Handle(cancellingInventoryHandler{takeSlot: -1, placeSlot: -1, dropSlot: 0})
	controlled := &stackRequestControllable{}

	h := newStackRequestHandler()
	err := h.handleDrop(&protocol.DropStackRequestAction{
		Count:  2,
		Source: stackRequestSlot(protocol.ContainerLevelEntity, 0, source),
	}, s, nil, controlled)
	if err == nil {
		t.Fatal("handleDrop() succeeded after source drop was cancelled")
	}
	assertStackEqual(t, opened, 0, source)
	if controlled.dropped != 0 {
		t.Fatalf("Drop() called for %d items after cancellation", controlled.dropped)
	}
}

func TestRejectedMultiActionRequestRollsBackEarlierChanges(t *testing.T) {
	s, opened, playerInv := stackRequestSession()
	first := item.NewStack(item.Diamond{}, 1)
	blocked := item.NewStack(item.Stick{}, 1)
	mustSetItem(t, opened, 0, first)
	mustSetItem(t, opened, 1, blocked)
	opened.Handle(cancellingInventoryHandler{takeSlot: 1, placeSlot: -1, dropSlot: -1})

	h := newStackRequestHandler()
	err := h.handleRequest(protocol.ItemStackRequest{
		RequestID: 12,
		Actions: []protocol.StackRequestAction{
			stackRequestTake(1, stackRequestSlot(protocol.ContainerLevelEntity, 0, first), stackRequestSlot(protocol.ContainerInventory, 0, item.Stack{})),
			stackRequestTake(1, stackRequestSlot(protocol.ContainerLevelEntity, 1, blocked), stackRequestSlot(protocol.ContainerInventory, 1, item.Stack{})),
		},
	}, s, nil, &stackRequestControllable{})
	if err == nil {
		t.Fatal("handleRequest() succeeded after its second action was cancelled")
	}
	assertStackEqual(t, opened, 0, first)
	assertStackEqual(t, opened, 1, blocked)
	assertStackEqual(t, playerInv, 0, item.Stack{})
	assertStackEqual(t, playerInv, 1, item.Stack{})
}

func TestRejectRestoresOriginalValueAfterRepeatedSlotChanges(t *testing.T) {
	s, opened, _ := stackRequestSession()
	original := item.NewStack(item.Diamond{}, 3)
	mustSetItem(t, opened, 0, original)
	h := newStackRequestHandler()
	slot := stackRequestSlot(protocol.ContainerLevelEntity, 0, original)
	h.currentRequest = 7
	h.setItemInSlot(slot, original.Grow(-1), s, nil)
	h.setItemInSlot(slot, original.Grow(-2), s, nil)
	h.reject(7, s, nil)
	assertStackEqual(t, opened, 0, original)
}

func stackRequestSession() (*Session, *inventory.Inventory, *inventory.Inventory) {
	opened := inventory.New(54, nil)
	playerInv := inventory.New(36, nil)
	s := &Session{
		inv:             playerInv,
		ui:              inventory.New(54, nil),
		offHand:         inventory.New(1, nil),
		packets:         make(chan packet.Packet, 8),
		closeBackground: make(chan struct{}),
	}
	s.openedWindow.Store(opened)
	s.containerOpened.Store(true)
	return s, opened, playerInv
}

func stackRequestTake(count byte, source, destination protocol.StackRequestSlotInfo) *protocol.TakeStackRequestAction {
	action := &protocol.TakeStackRequestAction{}
	action.Count = count
	action.Source = source
	action.Destination = destination
	return action
}

func newStackRequestHandler() *ItemStackRequestHandler {
	return &ItemStackRequestHandler{
		changes:         map[byte]map[byte]changeInfo{},
		responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{},
		current:         time.Now(),
	}
}

func stackRequestSlot(containerID byte, slot byte, stack item.Stack) protocol.StackRequestSlotInfo {
	return protocol.StackRequestSlotInfo{
		Container:      protocol.FullContainerName{ContainerID: containerID},
		Slot:           slot,
		StackNetworkID: item_id(stack),
	}
}

func mustSetItem(t *testing.T, inv *inventory.Inventory, slot int, stack item.Stack) {
	t.Helper()
	if err := inv.SetItem(slot, stack); err != nil {
		t.Fatal(err)
	}
}

func assertStackEqual(t *testing.T, inv *inventory.Inventory, slot int, want item.Stack) {
	t.Helper()
	got, err := inv.Item(slot)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("slot %d = %v, want %v", slot, got, want)
	}
}
