package player

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestCancelledFoodUseSynchronisesBeforeImmediateRestart(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
		p.UseItem()
		// A stop action can arrive between two rapid right clicks. It must clear
		// the using flag on the client as well as on the server.
		h := &session.PlayerActionHandler{}
		if err := h.Handle(&packet.PlayerAction{
			EntityRuntimeID: 1, ActionType: protocol.PlayerActionStopItemUseOn,
		}, session.Nop, p.tx, p); err != nil {
			t.Fatal(err)
		}
		if p.UsingItem() {
			t.Fatal("cancelled food is still in use")
		}
		p.UseItem()
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.UseItem()
		held, _ := p.HeldItems()
		if held.Count() != 1 || p.Food() != 14 {
			t.Fatalf("restarted eating = count:%d food:%d, want count:1 food:14", held.Count(), p.Food())
		}
		assertItemUseStates(t, conn, []bool{true, false, true, false})
	})
}

func TestFoodCompletionWaitsForDurationAndCommitsOnce(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
		for range 3 {
			p.UseItem()
			p.ReleaseItem()
		}
		p.UseItem()
		p.UseItem()
		held, _ := p.HeldItems()
		if held.Count() != 2 || p.Food() != 10 || !p.UsingItem() || !p.consumableUse.completionRequested {
			t.Fatal("early completion was consumed or discarded instead of deferred")
		}
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.tickConsumableUse()
		held, _ = p.HeldItems()
		if held.Count() != 1 || p.Food() != 14 || p.UsingItem() {
			t.Fatalf("completed eating = count:%d food:%d using:%v, want count:1 food:14 using:false", held.Count(), p.Food(), p.UsingItem())
		}
		p.UseItem()
		held, _ = p.HeldItems()
		if held.Count() != 1 || p.Food() != 14 || !p.UsingItem() {
			t.Fatal("next meal must start a separate cycle without consuming")
		}
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.UseItem()
		held, _ = p.HeldItems()
		if !held.Empty() || p.Food() != 18 || p.UsingItem() {
			t.Fatalf("second completion = count:%d food:%d using:%v, want count:0 food:18 using:false", held.Count(), p.Food(), p.UsingItem())
		}
		assertItemUseStates(t, conn, []bool{true, false, true, false, true, false, true, false, true, false})
	})
}

func TestFoodCompletionCannotStartUseAfterCancellation(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
		held, _ := p.HeldItems()
		if p.UsingItem() || held.Count() != 2 || p.Absorption() != 0 {
			t.Fatal("late simulation completion created a phantom cycle or consumed the replacement")
		}
		p.StartUsingConsumable()
		if !p.UsingItem() {
			t.Fatal("fresh press after rejection did not start a meal")
		}
	})
}

func TestFoodClientPressesCannotConsumeImmediately(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
		started, generation := p.usingSince, p.consumableUse.generation
		p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
		if p.usingSince != started || p.consumableUse.generation != generation || !p.consumableUse.completionRequested {
			t.Fatal("another player input reset the meal instead of requesting completion")
		}
		p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
		held, _ := p.HeldItems()
		if held.Count() != 2 || p.Absorption() != 0 {
			t.Fatal("rapid inputs consumed an apple")
		}
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.tickConsumableUse()
		held, _ = p.HeldItems()
		if held.Count() != 1 || p.Absorption() != 4 || p.UsingItem() {
			t.Fatal("deferred completion failed")
		}
		p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
		p.tickConsumableUse()
		held, _ = p.HeldItems()
		if held.Count() != 1 || p.UsingItem() {
			t.Fatal("duplicate completion restarted or consumed another cycle")
		}
	})
}

func TestGoldenAppleRefillsAbsorptionWithoutStacking(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		eat := func(food world.Item) {
			p.SetHeldItems(item.NewStack(food, 1), item.Stack{})
			p.UseItem()
			p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
			p.UseItem()
			if held, _ := p.HeldItems(); !held.Empty() {
				t.Fatalf("%T was not consumed", food)
			}
		}
		assertAbsorption := func(stage string, want float64) {
			t.Helper()
			if got := p.Absorption(); got != want {
				t.Fatalf("%s absorption = %v, want %v", stage, got, want)
			}
		}

		eat(item.GoldenApple{})
		assertAbsorption("first apple", 4)
		eat(item.GoldenApple{})
		assertAbsorption("second apple", 4)
		p.SetAbsorption(1)
		eat(item.GoldenApple{})
		assertAbsorption("refill", 4)

		eat(item.EnchantedApple{})
		assertAbsorption("enchanted apple", 16)
		p.SetAbsorption(8)
		eat(item.GoldenApple{})
		assertAbsorption("stronger absorption", 8)
	})
}

func TestFoodPendingCompletionCancelsOnReleaseAndAllowsPickup(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
		p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
		p.ReleaseItem()
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.tickConsumableUse()
		held, _ := p.HeldItems()
		if held.Count() != 2 || p.Absorption() != 0 {
			t.Fatal("released pending meal was consumed")
		}
		p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
		p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
		if _, err := p.Inventory().AddItem(item.NewStack(item.GoldenApple{}, 3)); err != nil {
			t.Fatal(err)
		}
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.tickConsumableUse()
		held, _ = p.HeldItems()
		if held.Count() != 4 || p.Absorption() != 4 {
			t.Fatal("matching pickup cancelled or duplicated consumption")
		}
	})
}

func TestFoodRejectsWrongHeldIdentityButAcceptsCountCorrections(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		p.UseItem()
		valid, err := p.session().VerifyHeldItemUse(0, item.NewStack(item.Snowball{}, 1), p)
		if err != nil || valid || p.UsingItem() {
			t.Fatalf("wrong identity accepted: valid=%t error=%v", valid, err)
		}
		held, _ := p.HeldItems()
		if held.Count() != 2 || p.Absorption() != 0 {
			t.Fatal("correction consumed an apple")
		}
		valid, err = p.session().VerifyHeldItemUse(0, item.NewStack(item.GoldenApple{}, 1), p)
		if err != nil || !valid {
			t.Fatalf("count-only difference rejected: %v", err)
		}
		p.SetHeldItems(item.Stack{}, item.Stack{})
		valid, err = p.session().VerifyHeldItemUse(0, item.NewStack(item.GoldenApple{}, 1), p)
		if err != nil || valid {
			t.Fatal("a stale food request was accepted against an empty server slot")
		}
	})
}

func TestFoodLethalEffectCannotConsumeOrInsertItemsAfterDeath(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.addHealth(-19)
		p.SetHeldItems(item.NewStack(item.Potion{Type: potion.Harming()}, 1), item.Stack{})
		p.UseItem()
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.UseItem()
		held, _ := p.HeldItems()
		if !p.Dead() || !held.Empty() || p.UsingItem() {
			t.Fatalf("post-death state: dead=%t held=%v using=%t", p.Dead(), held, p.UsingItem())
		}
	})
}

func TestFoodPacketPathsPreserveStartAndCompletion(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		name := "transaction"
		if embedded {
			name = "auth input"
		}
		t.Run(name, func(t *testing.T) {
			withStateTestPlayer(t, func(p *Player, _ *stateConn) {
				p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
				if embedded {
					p.UseItem()
				}
				emptyPrediction := false
				send := func(trigger uint32, startFlag bool) {
					held, _ := p.HeldItems()
					id, meta, _ := world.ItemRuntimeID(held.Item())
					data := protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickAir, TriggerType: trigger, Hand: protocol.HandSlotMainHand,
						HeldItem: protocol.ItemInstance{Stack: protocol.ItemStack{ItemType: protocol.ItemType{NetworkID: id, MetadataValue: uint32(meta)}, Count: uint16(held.Count())}}}
					if emptyPrediction {
						data.HeldItem.Stack.Count = 0
					}
					var err error
					if embedded {
						flags := protocol.NewInputFlags(128)
						flags.Set(packet.InputFlagPerformItemInteraction)
						if startFlag {
							flags.Set(packet.InputFlagStartUsingItem)
						}
						var actions protocol.Optional[[]protocol.PlayerBlockAction]
						if startFlag {
							flags.Set(packet.InputFlagPerformBlockActions)
							actions = protocol.Option([]protocol.PlayerBlockAction{{Action: protocol.PlayerActionStopItemUseOn}})
						}
						pos := p.Position()
						err = (session.PlayerAuthInputHandler{}).Handle(&packet.PlayerAuthInput{InputData: flags, BlockActions: actions, ItemInteractionData: protocol.Option(data), Position: mgl32.Vec3{float32(pos[0]), float32(pos[1] + 1.62), float32(pos[2])}}, p.session(), p.tx, p)
					} else {
						err = (&session.InventoryTransactionHandler{}).Handle(&packet.InventoryTransaction{TransactionData: &data}, p.session(), p.tx, p)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				trigger := uint32(protocol.TriggerTypePlayerInput)
				if embedded {
					trigger = protocol.TriggerTypeUnknown
				}
				send(trigger, true)
				if !p.UsingItem() {
					t.Fatal("start input was not accepted")
				}
				p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
				send(protocol.TriggerTypeSimulationTick, false)
				held, _ := p.HeldItems()
				if held.Count() != 1 || p.UsingItem() || p.Absorption() != 4 {
					t.Fatal("completion did not apply once")
				}
				send(protocol.TriggerTypeSimulationTick, false)
				if p.UsingItem() {
					t.Fatal("duplicate wire completion started another cycle")
				}
				if !embedded {
					stopFood(t, p)
				}
				send(trigger, true)
				p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
				emptyPrediction = true
				send(protocol.TriggerTypeSimulationTick, false)
				held, _ = p.HeldItems()
				if !held.Empty() || p.UsingItem() {
					t.Fatal("predicted empty last-apple completion was rejected")
				}
			})
		})
	}
}

func TestFoodPacketsDoNotUseTriggerAsMealPhase(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, start := range []uint32{protocol.TriggerTypeUnknown, protocol.TriggerTypePlayerInput} {
			for _, completion := range []uint32{protocol.TriggerTypeUnknown, protocol.TriggerTypePlayerInput, protocol.TriggerTypeSimulationTick} {
				t.Run(fmt.Sprintf("embedded=%t/start=%d/completion=%d", embedded, start, completion), func(t *testing.T) {
					withStateTestPlayer(t, func(p *Player, conn *stateConn) {
						p.SetHeldItems(item.NewStack(item.GoldenApple{}, 1), item.Stack{})
						send := func(trigger uint32, finish bool) {
							held, _ := p.HeldItems()
							id, meta, _ := world.ItemRuntimeID(held.Item())
							data := protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickAir, TriggerType: trigger,
								Hand: protocol.HandSlotMainHand, HeldItem: protocol.ItemInstance{Stack: protocol.ItemStack{
									ItemType: protocol.ItemType{NetworkID: id, MetadataValue: uint32(meta)}, Count: uint16(held.Count()),
								}}}
							if finish {
								data.HeldItem.Stack.Count = 0 // The client predicted its last apple.
							}
							var err error
							if embedded {
								flags := protocol.NewInputFlags(128)
								flags.Set(packet.InputFlagPerformItemInteraction)
								var actions protocol.Optional[[]protocol.PlayerBlockAction]
								if finish {
									flags.Set(packet.InputFlagPerformBlockActions)
									actions = protocol.Option([]protocol.PlayerBlockAction{{Action: protocol.PlayerActionStopItemUseOn}})
								}
								pos := p.Position()
								err = (session.PlayerAuthInputHandler{}).Handle(&packet.PlayerAuthInput{InputData: flags,
									ItemInteractionData: protocol.Option(data), BlockActions: actions,
									Position: mgl32.Vec3{float32(pos[0]), float32(pos[1] + 1.62), float32(pos[2])}}, p.session(), p.tx, p)
							} else {
								err = (&session.InventoryTransactionHandler{}).Handle(&packet.InventoryTransaction{TransactionData: &data}, p.session(), p.tx, p)
							}
							if err != nil {
								t.Fatal(err)
							}
						}
						send(start, false)
						if !p.UsingItem() {
							t.Fatalf("embedded=%t: start with trigger %d was rejected", embedded, start)
						}
						p.Tick(p.tx, 4)
						p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
						send(completion, true)
						held, _ := p.HeldItems()
						if !held.Empty() || p.UsingItem() || p.Absorption() != 4 {
							t.Fatalf("embedded=%t start=%d completion=%d: count=%d using=%t absorption=%v", embedded, start, completion, held.Count(), p.UsingItem(), p.Absorption())
						}
						foundFeed := false
						deadline := time.Now().Add(time.Second)
						for !foundFeed && time.Now().Before(deadline) {
							for _, pk := range conn.snapshot() {
								if event, ok := pk.(*packet.ActorEvent); ok && event.EventType == packet.ActorEventFeed {
									foundFeed = true
								}
							}
							if !foundFeed {
								time.Sleep(time.Millisecond)
							}
						}
						if !foundFeed {
							t.Fatal("accepted use did not produce eating feedback")
						}
					})
				})
			}
		}
	}
}

func TestFoodPairedFinishSurvivesItsStop(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		want    int
	}{
		{"near the deadline", item.DefaultConsumeDuration - 100*time.Millisecond, 1},
		{"mid bite", item.DefaultConsumeDuration / 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withStateTestPlayer(t, func(p *Player, _ *stateConn) {
				p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
				sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypePlayerInput), true)
				p.usingSince = time.Now().Add(-tc.elapsed)
				sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypeSimulationTick), false, protocol.PlayerActionStopItemUseOn)
				if p.Absorption() != 0 {
					t.Fatal("paired stop skipped the remaining eating time")
				}
				p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
				p.tickConsumableUse()
				p.tickConsumableUse()
				held, _ := p.HeldItems()
				if held.Count() != tc.want || p.UsingItem() || p.Absorption() != float64((2-tc.want)*4) {
					t.Fatalf("after the deadline: stack=%d absorption=%v using=%v", held.Count(), p.Absorption(), p.UsingItem())
				}
			})
		})
	}
}

func TestFoodReleaseSettlesPendingBiteBeforeWorldTick(t *testing.T) {
	for _, useAction := range []bool{false, true} {
		withStateTestPlayer(t, func(p *Player, _ *stateConn) {
			p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
			p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
			p.UseItemWithTrigger(protocol.TriggerTypeSimulationTick)
			p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
			if useAction {
				stopFood(t, p)
			} else {
				held := foodClick(p, protocol.TriggerTypePlayerInput).HeldItem
				pk := &packet.InventoryTransaction{TransactionData: &protocol.ReleaseItemTransactionData{
					Hand: protocol.HandSlotMainHand, HeldItem: held,
				}}
				if err := (&session.InventoryTransactionHandler{}).Handle(pk, p.session(), p.tx, p); err != nil {
					t.Fatal(err)
				}
			}
			held, _ := p.HeldItems()
			if held.Count() != 1 || p.UsingItem() || p.Absorption() != 4 {
				t.Fatal("eligible bite was lost at release")
			}
			p.UseItemWithTrigger(protocol.TriggerTypePlayerInput)
			if !p.UsingItem() {
				t.Fatal("fresh press after release was ignored")
			}
		})
	}
}

func TestFoodNextPressCanFollowDeferredFinish(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, _ *stateConn) {
		p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
		sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypePlayerInput), true)
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration + 100*time.Millisecond)
		sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypeSimulationTick), false, protocol.PlayerActionStopItemUseOn)
		sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypePlayerInput), true)
		p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
		p.tickConsumableUse()
		if held, _ := p.HeldItems(); held.Count() != 1 || !p.UsingItem() || p.consumableUse.completionRequested {
			t.Fatal("next press did not open a fresh bite after the old bite finished")
		}
	})
}

func TestFoodRepeatedFinishDoesNotStartAnotherBite(t *testing.T) {
	for _, trigger := range []uint32{protocol.TriggerTypeUnknown, protocol.TriggerTypePlayerInput, protocol.TriggerTypeSimulationTick} {
		withStateTestPlayer(t, func(p *Player, _ *stateConn) {
			p.SetHeldItems(item.NewStack(item.GoldenApple{}, 3), item.Stack{})
			sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypePlayerInput), true)
			p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
			sendFoodFrame(t, p, foodClick(p, trigger), false, protocol.PlayerActionStopItemUseOn)
			sendFoodFrame(t, p, foodClick(p, trigger), false)
			if held, _ := p.HeldItems(); held.Count() != 2 || p.UsingItem() {
				t.Fatalf("repeated finish with trigger %d started or completed a new bite", trigger)
			}
			sendFoodFrame(t, p, foodClick(p, protocol.TriggerTypeUnknown), true)
			if !p.UsingItem() || p.consumableUse.completionRequested {
				t.Fatal("explicit start did not open its own bite")
			}
		})
	}
}

func TestFoodBlockUseKeepsTimer(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		withStateTestPlayer(t, func(p *Player, _ *stateConn) {
			if redirect {
				p.Handle(redirectFoodOnBlock{})
			}
			pos := cube.PosFromVec3(p.Position()).Side(cube.FaceDown)
			p.tx.SetBlock(pos, block.Stone{}, nil)
			p.SetHeldItems(item.NewStack(item.GoldenApple{}, 2), item.Stack{})
			click := foodClick(p, protocol.TriggerTypePlayerInput)
			click.ActionType = protocol.UseItemActionClickBlock
			click.BlockFace = int32(cube.FaceUp)
			click.BlockPosition = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
			sendFoodFrame(t, p, click, true, protocol.PlayerActionStartItemUseOn)
			if !p.UsingItem() {
				t.Fatalf("redirect=%v: block start cancelled the bite", redirect)
			}
			p.usingSince = time.Now().Add(-item.DefaultConsumeDuration)
			click.TriggerType = protocol.TriggerTypeSimulationTick
			sendFoodFrame(t, p, click, false, protocol.PlayerActionStopItemUseOn)
			if held, _ := p.HeldItems(); held.Count() != 1 || p.UsingItem() || p.Absorption() != 4 {
				t.Fatalf("redirect=%v: continuing use on stone did not finish the bite", redirect)
			}
		})
	}
}

type redirectFoodOnBlock struct{ NopHandler }

func (redirectFoodOnBlock) HandleItemUseOnBlock(ctx *Context, _ cube.Pos, _ cube.Face, _ mgl64.Vec3) {
	ctx.UseItemInAir()
}

func stopFood(t *testing.T, p *Player) {
	t.Helper()
	err := (&session.PlayerActionHandler{}).Handle(&packet.PlayerAction{
		EntityRuntimeID: 1, ActionType: protocol.PlayerActionStopItemUseOn,
	}, p.session(), p.tx, p)
	if err != nil {
		t.Fatal(err)
	}
}

func foodClick(p *Player, trigger uint32) protocol.UseItemTransactionData {
	held, _ := p.HeldItems()
	id, meta, _ := world.ItemRuntimeID(held.Item())
	return protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickAir, TriggerType: trigger,
		Hand: protocol.HandSlotMainHand, HeldItem: protocol.ItemInstance{Stack: protocol.ItemStack{
			ItemType: protocol.ItemType{NetworkID: id, MetadataValue: uint32(meta)}, Count: uint16(held.Count()),
		}}}
}

func sendFoodFrame(t *testing.T, p *Player, click protocol.UseItemTransactionData, start bool, actions ...int32) {
	t.Helper()
	flags := protocol.NewInputFlags(128)
	flags.Set(packet.InputFlagPerformItemInteraction)
	if start {
		flags.Set(packet.InputFlagStartUsingItem)
	}
	pos := p.Position()
	pk := &packet.PlayerAuthInput{InputData: flags, ItemInteractionData: protocol.Option(click),
		Position: mgl32.Vec3{float32(pos[0]), float32(pos[1] + 1.62), float32(pos[2])}}
	if len(actions) != 0 {
		pk.InputData.Set(packet.InputFlagPerformBlockActions)
		blockActions := make([]protocol.PlayerBlockAction, len(actions))
		for i, action := range actions {
			blockActions[i].Action = action
		}
		pk.BlockActions = protocol.Option(blockActions)
	}
	if err := (session.PlayerAuthInputHandler{}).Handle(pk, p.session(), p.tx, p); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledBowReleaseSynchronisesStoppedUse(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.Handle(cancelItemRelease{})
		p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
		_ = p.Inventory().SetItem(1, item.NewStack(item.Arrow{}, 2))
		p.UseItem()
		p.usingSince = time.Now().Add(-time.Second)
		p.ReleaseItem()
		if p.UsingItem() {
			t.Fatal("cancelled bow release is still in use")
		}
		arrows, _ := p.Inventory().Item(1)
		if arrows.Count() != 2 {
			t.Fatal("cancelled bow release consumed an arrow")
		}
		assertItemUseStates(t, conn, []bool{true, false})
	})
}

type cancelItemRelease struct{ NopHandler }

func (cancelItemRelease) HandleItemRelease(ctx *Context, _ item.Stack, _ time.Duration) {
	ctx.Cancel()
}

func assertItemUseStates(t *testing.T, conn *stateConn, want []bool) {
	t.Helper()
	var got []bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got = got[:0]
		for _, pk := range conn.snapshot() {
			if update, ok := pk.(*packet.SetActorData); ok {
				got = append(got, update.EntityMetadata.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagUsingItem))
			}
		}
		if slices.Equal(got, want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client item-use states = %v, want %v", got, want)
}
