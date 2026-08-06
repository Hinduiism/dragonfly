package session

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	// enchantingInputSlot is the slot index of the input item in the enchanting table.
	enchantingInputSlot = 0x0e
	// enchantingLapisSlot is the slot index of the lapis in the enchanting table.
	enchantingLapisSlot = 0x0f
)

type enchantingCandidate struct {
	enchantment item.Enchantment
	weight      int
}

type enchantingOffer struct {
	slot           uint32
	requirement    int
	experienceCost int
	lapisCost      int
	enchantments   []item.Enchantment
}

// handleEnchant handles the enchantment of an item using the CraftRecipe stack request action.
func (h *ItemStackRequestHandler) handleEnchant(a *protocol.CraftRecipeStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	// First ensure we have an input and only one input.
	input, err := s.ui.Item(enchantingInputSlot)
	if err != nil {
		return err
	}
	if input.Count() > 1 {
		return fmt.Errorf("enchanting tables only accept one item at a time")
	}

	// Determine the available enchantments using the session's enchantment seed.
	offers := s.determineAvailableEnchantments(tx, c, *s.openedPos.Load(), input)
	offer, err := selectEnchantingOffer(offers, a.RecipeNetworkID)
	if err != nil {
		return err
	}

	// If we don't have infinite resources, we need to deduct Lapis Lazuli and experience.
	if !c.GameMode().CreativeInventory() {
		// First ensure that the experience level is both underneath the requirement and the cost.
		if c.ExperienceLevel() < offer.requirement {
			return fmt.Errorf("not enough levels to meet requirement")
		}
		if c.ExperienceLevel() < offer.experienceCost {
			return fmt.Errorf("not enough levels to meet cost")
		}

		// Then ensure that the player has input Lapis Lazuli, and enough of it to meet the cost.
		lapis, err := s.ui.Item(enchantingLapisSlot)
		if err != nil {
			return err
		}
		if _, ok := lapis.Item().(item.LapisLazuli); !ok {
			return fmt.Errorf("lapis lazuli was not input")
		}
		if lapis.Count() < offer.lapisCost {
			return fmt.Errorf("not enough lapis lazuli to meet cost")
		}

		// Deduct the experience and Lapis Lazuli.
		c.SetExperienceLevel(c.ExperienceLevel() - offer.experienceCost)
		h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerEnchantingMaterial},
			Slot:      enchantingLapisSlot,
		}, lapis.Grow(-offer.lapisCost), s, tx)
	}

	// Reset the enchantment seed so different enchantments can be selected.
	c.ResetEnchantmentSeed()

	// Clear the existing input item, and apply the new item into the crafting result slot of the UI. The client will
	// automatically move the item into the input slot.
	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerEnchantingInput},
		Slot:      enchantingInputSlot,
	}, item.Stack{}, s, tx)

	return h.createResults(s, tx, input.WithEnchantments(offer.enchantments...))
}

func selectEnchantingOffer(offers []enchantingOffer, recipeNetworkID uint32) (enchantingOffer, error) {
	if recipeNetworkID > 2 {
		return enchantingOffer{}, fmt.Errorf("invalid recipe network id: %d", recipeNetworkID)
	}
	for _, offer := range offers {
		if offer.slot != recipeNetworkID {
			continue
		}
		if len(offer.enchantments) == 0 {
			break
		}
		return offer, nil
	}
	return enchantingOffer{}, fmt.Errorf("enchantment option %d is unavailable", recipeNetworkID)
}

// sendEnchantmentOptions sends a list of available enchantments to the client based on the client's enchantment seed
// and nearby bookshelves.
func (s *Session) sendEnchantmentOptions(tx *world.Tx, c Controllable, pos cube.Pos, stack item.Stack) {
	// First determine the available enchantments for the given item stack.
	offers := s.determineAvailableEnchantments(tx, c, pos, stack)

	// Build the protocol variant of the enchantment options.
	options := make([]protocol.EnchantmentOption, 0, 3)
	for _, offer := range offers {
		if len(offer.enchantments) == 0 {
			continue
		}
		// First build the enchantment instances for each selected enchantment.
		enchants := make([]protocol.EnchantmentInstance, 0, len(offer.enchantments))
		for _, enchant := range offer.enchantments {
			id, _ := item.EnchantmentID(enchant.Type())
			enchants = append(enchants, protocol.EnchantmentInstance{
				Type:  byte(id),
				Level: byte(enchant.Level()),
			})
		}

		// Then build the enchantment option. We can use the slot as the RecipeNetworkID, since the IDs seem to be unique
		// to enchanting tables only. We also only need to set the middle index of Enchantments. The other two serve
		// an unknown purpose and can cause various unexpected issues.
		options = append(options, protocol.EnchantmentOption{
			Name:            enchantNames[rand.IntN(len(enchantNames))],
			Cost:            uint8(offer.requirement),
			RecipeNetworkID: offer.slot,
			Enchantments: protocol.ItemEnchantments{
				Slot:         int32(offer.slot),
				Enchantments: [3][]protocol.EnchantmentInstance{1: enchants},
			},
		})
	}

	// Send the enchantment options to the client.
	s.writePacket(&packet.PlayerEnchantOptions{Options: options})
}

// determineAvailableEnchantments returns a list of pseudo-random enchantments for the given item stack.
func (s *Session) determineAvailableEnchantments(tx *world.Tx, c Controllable, pos cube.Pos, stack item.Stack) []enchantingOffer {
	// First ensure that the item is enchantable and does not already have any enchantments.
	enchantable, ok := stack.Item().(item.Enchantable)
	if !ok {
		// We can't enchant this item.
		return nil
	}
	if len(stack.Enchantments()) > 0 {
		// We can't enchant this item.
		return nil
	}

	// Search for bookshelves around the enchanting table. Bookshelves help boost the value of the enchantments that
	// are selected, resulting in enchantments that are rarer but also more expensive.
	seed := uint64(c.EnchantmentSeed())
	random := rand.New(rand.NewPCG(seed, seed))
	bookshelves := searchBookshelves(tx, pos)
	value := enchantable.EnchantmentValue()

	// Calculate the base cost, used to calculate the upper, middle, and lower level costs.
	baseCost := random.IntN(8) + 1 + (bookshelves >> 1) + random.IntN(bookshelves+1)

	// Calculate the upper, middle, and lower level costs.
	upperLevelCost := max(baseCost/3, 1)
	middleLevelCost := baseCost*2/3 + 1
	lowerLevelCost := max(baseCost, bookshelves*2)

	// Create a list of available enchantments for each slot. Keep the generated
	// enchanting power separate from the player-facing requirement and payment.
	levelCosts := [...]int{upperLevelCost, middleLevelCost, lowerLevelCost}
	offers := make([]enchantingOffer, len(levelCosts))
	for slot, levelCost := range levelCosts {
		enchantments := createEnchantments(random, stack, value, levelCost, s.conf.EnchantingTablePolicy)
		offers[slot] = newEnchantingOffer(slot, levelCost, enchantments, s.conf.EnchantingTablePolicy)
	}
	return offers
}

func newEnchantingOffer(slot, levelCost int, enchantments []item.Enchantment, policy *item.EnchantingTablePolicy) enchantingOffer {
	requirement, experienceCost := levelCost, slot+1
	if len(enchantments) > 0 && policy != nil {
		primary := enchantments[0]
		if rule, ok := policy.Rule(primary.Type()); ok {
			if configuredCost, overridden := rule.ExperienceCost(primary.Level()); overridden {
				requirement, experienceCost = configuredCost, configuredCost
			}
		}
	}
	return enchantingOffer{
		slot:           uint32(slot),
		requirement:    requirement,
		experienceCost: experienceCost,
		lapisCost:      slot + 1,
		enchantments:   enchantments,
	}
}

// treasureEnchantment represents an enchantment that may be a treasure enchantment.
type treasureEnchantment interface {
	item.EnchantmentType
	Treasure() bool
}

// createEnchantments creates a list of enchantments for the given item stack and returns them.
func createEnchantments(random *rand.Rand, stack item.Stack, value, level int, policy *item.EnchantingTablePolicy) []item.Enchantment {
	// Calculate the "random bonus" for this level. This factor is used in calculating the enchantment cost, used
	// during the selection of enchantments.
	randomBonus := (random.Float64() + random.Float64() - 1.0) * 0.15

	// Calculate the enchantment cost and clamp it to ensure it is always at least one with triangular distribution.
	cost := level + 1 + random.IntN(value/4+1) + random.IntN(value/4+1)
	cost = clamp(int(math.Round(float64(cost)+float64(cost)*randomBonus)), 1, math.MaxInt32)

	availableEnchants := availableEnchantingCandidates(stack, cost, policy)
	if len(availableEnchants) == 0 {
		// No available enchantments, so we can't really do much here.
		return nil
	}

	// Now we need to select the enchantments.
	selectedEnchants := make([]item.Enchantment, 0, len(availableEnchants))

	// Select the first enchantment using a weighted random algorithm, favouring enchantments that have a higher weight.
	// These weights are based on the enchantment's rarity, with common and uncommon enchantments having a higher weight
	// than rare and very rare enchantments.
	candidate := weightedRandomEnchantment(random, availableEnchants)
	selectedEnchants = append(selectedEnchants, candidate.enchantment)

	// Remove the selected enchantment from the list of available enchantments, so we don't select it again.
	ind := slices.Index(availableEnchants, candidate)
	availableEnchants = slices.Delete(availableEnchants, ind, ind+1)

	// Based on the cost, select a random amount of additional enchantments.
	for random.IntN(50) <= cost {
		// Ensure that we don't have any conflicting enchantments. If so, remove them from the list of available
		// enchantments.
		lastEnchant := selectedEnchants[len(selectedEnchants)-1]
		if availableEnchants = sliceutil.Filter(availableEnchants, func(candidate enchantingCandidate) bool {
			return lastEnchant.Type().CompatibleWithEnchantment(candidate.enchantment.Type())
		}); len(availableEnchants) == 0 {
			// We've exhausted all available enchantments.
			break
		}

		// Select another enchantment using the same weighted random algorithm.
		candidate = weightedRandomEnchantment(random, availableEnchants)
		selectedEnchants = append(selectedEnchants, candidate.enchantment)

		// Remove the selected enchantment from the list of available enchantments, so we don't select it again.
		ind = slices.Index(availableEnchants, candidate)
		availableEnchants = slices.Delete(availableEnchants, ind, ind+1)

		// Halve the cost, so we have a lower chance of selecting another enchantment.
		cost /= 2
	}
	return selectedEnchants
}

func availableEnchantingCandidates(stack item.Stack, cost int, policy *item.EnchantingTablePolicy) []enchantingCandidate {
	// Books are applicable to all enchantments.
	it := stack.Item()
	_, book := it.(item.Book)
	available := make([]enchantingCandidate, 0, len(item.Enchantments()))
	for _, enchant := range item.Enchantments() {
		maximumLevel, weight := enchant.MaxLevel(), enchant.Rarity().Weight()
		if policy == nil {
			if t, ok := enchant.(treasureEnchantment); ok && t.Treasure() {
				// We then have to ensure that the enchantment is not a treasure enchantment, as those cannot be selected through
				// the enchanting table.
				continue
			}
		} else if rule, ok := policy.Rule(enchant); !ok {
			continue
		} else {
			maximumLevel, weight = rule.MaxLevel, rule.Weight
		}
		if !book && !enchant.CompatibleWithItem(it) {
			// The enchantment is not compatible with the item.
			continue
		}

		// Now iterate through each possible level of the enchantment.
		for i := maximumLevel; i > 0; i-- {
			// Use the level to calculate the minimum and maximum costs for this enchantment.
			if minCost, maxCost := enchant.Cost(i); cost >= minCost && cost <= maxCost {
				// If the cost is within the bounds, add the enchantment to the list of available enchantments.
				available = append(available, enchantingCandidate{
					enchantment: item.NewEnchantment(enchant, i),
					weight:      weight,
				})
				break
			}
		}
	}
	return available
}

// searchBookshelves searches for nearby bookshelves around the position passed, and returns the amount found.
func searchBookshelves(tx *world.Tx, pos cube.Pos) (shelves int) {
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			for y := 0; y <= 1; y++ {
				if x == 0 && z == 0 {
					// Ignore the centre block.
					continue
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Air); !ok {
					// There must be a one block space between the bookshelf and the player.
					continue
				}

				// Check for a bookshelf two blocks away.
				if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z * 2})).(block.Bookshelf); ok {
					shelves++
				}
				if x != 0 && z != 0 {
					// Check for a bookshelf two blocks away on the X axis.
					if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z})).(block.Bookshelf); ok {
						shelves++
					}
					// Check for a bookshelf two blocks away on the Z axis.
					if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z * 2})).(block.Bookshelf); ok {
						shelves++
					}
				}

				if shelves >= 15 {
					// We've found enough bookshelves.
					return 15
				}
			}
		}
	}
	return shelves
}

// weightedRandomEnchantment returns a random enchantment from the given list of enchantments using the rarity weight of
// each enchantment.
func weightedRandomEnchantment(rs *rand.Rand, enchants []enchantingCandidate) enchantingCandidate {
	var totalWeight int
	for _, e := range enchants {
		totalWeight += e.weight
	}
	return weightedEnchantmentAt(enchants, rs.IntN(totalWeight))
}

func weightedEnchantmentAt(enchants []enchantingCandidate, value int) enchantingCandidate {
	for _, e := range enchants {
		value -= e.weight
		if value < 0 {
			return e
		}
	}
	panic("should never happen")
}

// clamp clamps a value into the given range.
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
