package player

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type proofHooks struct {
	NopHandler
	enabled, credit, programmatic        bool
	cancelSlot, cancelAttack, cancelHurt bool
	overrideCrit, critical               bool
	policy                               HitPolicy
	outcomes                             []DamageOutcome
	slots, deathCalls                    int
	order                                []string
	cooldown                             time.Duration
}

var (
	_ MeleeCooldownHandler   = (*proofHooks)(nil)
	_ DamageOutcomeHandler   = (*proofHooks)(nil)
	_ HeldSlotChangeObserver = (*proofHooks)(nil)
	_ ItemCooldownHandler    = (*proofHooks)(nil)
)

func (h *proofHooks) HandleHeldSlotChange(ctx *Context, _, _ int) {
	if h.cancelSlot {
		ctx.Cancel()
	}
}
func (h *proofHooks) HandleHeldSlotChanged(_ *Player, _, _ int) {
	h.slots++
	if h.enabled && !h.programmatic {
		h.credit = true
	}
}
func (h *proofHooks) HandleMeleeCooldown(_ *Context, _ *Player) bool {
	return h.enabled && h.credit
}
func (h *proofHooks) HandleAttackEntity(ctx *Context, _ world.Entity, _, _ *float64, critical *bool) {
	if h.cancelAttack {
		ctx.Cancel()
	}
	if h.overrideCrit {
		*critical = h.critical
	}
}
func (h *proofHooks) HandleHit(_ *Context, _ world.DamageSource) HitPolicy { return h.policy }
func (h *proofHooks) HandleHurt(ctx *Context, _ *float64, _ bool, _ *time.Duration, _ world.DamageSource) {
	if h.cancelHurt {
		ctx.Cancel()
	}
}
func (h *proofHooks) HandleDamageOutcome(_ *Player, out DamageOutcome) {
	h.outcomes = append(h.outcomes, out)
	h.order = append(h.order, "damage")
	if src, ok := out.Source.(entity.AttackDamageSource); ok && out.DirectMelee && out.MeleeCooldownOverride {
		if attacker, ok := src.Attacker.(*Player); ok {
			if ah, ok := attacker.Handler().(*proofHooks); ok {
				ah.credit = false
			}
		}
	}
}
func (h *proofHooks) HandleDeath(_ *Player, _ world.DamageSource, keep *bool) {
	h.deathCalls++
	h.order = append(h.order, "death")
	*keep = true
}
func (h *proofHooks) HandleItemCooldown(_ *Player, _ world.Item, native time.Duration) time.Duration {
	if h.cooldown != 0 {
		return h.cooldown
	}
	return native
}

func proofPair(t *testing.T, f func(*world.Tx, *Player, *Player, *proofHooks, *proofHooks)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		cfg := Config{Health: 20, MaxHealth: 20, GameMode: world.GameModeSurvival}
		cfg.Position = mgl64.Vec3{1, 64, -2}
		a := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, cfg)).(*Player)
		cfg.Position = mgl64.Vec3{1, 64, 0}
		v := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, cfg)).(*Player)
		a.Teleport(mgl64.Vec3{1, 64, -2})
		ah, vh := &proofHooks{enabled: true}, &proofHooks{}
		a.Handle(ah)
		v.Handle(vh)
		f(tx, a, v, ah, vh)
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWarProofSwitchPassesBothGatesOnce(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
		sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
		_ = a.Inventory().SetItem(0, sword)
		_ = a.Inventory().SetItem(1, sword)
		if !a.AttackEntity(v) {
			t.Fatal("ordinary first hit rejected")
		}
		first := v.Health()
		if a.AttackEntity(v) || v.Health() != first {
			t.Fatal("ordinary warm hit changed damage")
		}
		if err := a.SetHeldSlot(1); err != nil {
			t.Fatal(err)
		}
		if !a.AttackEntity(v) || v.Health() >= first {
			t.Fatal("switch failed to pass both gates")
		}
		second := v.Health()
		if ah.credit || len(vh.outcomes) != 2 || !vh.outcomes[1].MeleeCooldownOverride {
			t.Fatal("credit/outcome mismatch")
		}
		if a.AttackEntity(v) || v.Health() != second {
			t.Fatal("one switch authorized multiple attacks")
		}
		if got := vh.outcomes[1].HealthLost; math.Abs(got-sword.AttackDamage()) > 1e-9 {
			t.Fatalf("fresh damage=%v want=%v", got, sword.AttackDamage())
		}
	})
}

func TestWarProofSlotCommitAndEquipmentGuard(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, _ *Player, ah, _ *proofHooks) {
		_ = a.SetHeldSlot(0)
		if ah.slots != 0 || ah.credit {
			t.Fatal("same slot gave credit")
		}
		if a.SetHeldSlot(9) == nil {
			t.Fatal("invalid slot accepted")
		}
		ah.cancelSlot = true
		_ = a.SetHeldSlot(1)
		if ah.slots != 0 || ah.credit || *a.heldSlot != 0 {
			t.Fatal("cancelled slot gave credit")
		}
		ah.cancelSlot = false
		ah.programmatic = true
		_ = a.SetHeldSlot(1)
		if ah.credit {
			t.Fatal("loadout gave credit")
		}
		ah.programmatic = false
		_ = a.SetHeldSlot(2)
		_ = a.SetHeldSlot(3)
		if !ah.credit || ah.slots != 3 {
			t.Fatal("actual change not observed")
		}
	})
}

func TestWarProofSwitchCannotBypassProtectionOrCancellation(t *testing.T) {
	for _, kind := range []string{"spawn", "dead", "cancel_attack", "cancel_hurt", "blocked", "reach", "spectator"} {
		t.Run(kind, func(t *testing.T) {
			proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
				_ = a.SetHeldSlot(1)
				switch kind {
				case "spawn":
					v.directMelee = newDirectMeleeState(true)
				case "dead":
					v.health.SetHealth(0)
				case "cancel_attack":
					ah.cancelAttack = true
				case "cancel_hurt":
					vh.cancelHurt = true
				case "blocked":
					vh.policy = HitBlocked
				case "reach":
					a.Teleport(mgl64.Vec3{1, 64, -30})
				case "spectator":
					v.SetGameMode(world.GameModeSpectator)
				}
				before := v.Health()
				if a.AttackEntity(v) || v.Health() != before || !ah.credit || len(vh.outcomes) != 0 {
					t.Fatal("protected attempt consumed state")
				}
			})
		})
	}
}

func TestWarProofKnockbackOnlyDoesNotConsumeSwitch(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
		vh.policy = HitKnockbackOnly
		_ = a.SetHeldSlot(1)
		if !a.AttackEntity(v) || v.Health() != 20 || !ah.credit || len(vh.outcomes) != 0 {
			t.Fatal("boost altered normal damage state")
		}
	})
}

func TestWarProofSwitchOffAndOtherImmunityGroups(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, _ *proofHooks) {
		v.projectileImmuneUntil = time.Now().Add(time.Minute)
		v.projectileLastDamage = 99
		projectileUntil := v.projectileImmuneUntil
		if !a.AttackEntity(v) {
			t.Fatal("first hit")
		}
		ah.enabled = false
		_ = a.SetHeldSlot(1)
		before := v.Health()
		if a.AttackEntity(v) || v.Health() != before {
			t.Fatal("disabled switching bypassed gate")
		}
		ah.enabled = true
		_ = a.SetHeldSlot(2)
		if !a.AttackEntity(v) {
			t.Fatal("valid switch rejected")
		}
		if v.projectileImmuneUntil != projectileUntil || v.projectileLastDamage != 99 {
			t.Fatal("switch changed projectile immunity")
		}
		if v.immuneUntil.Before(time.Now()) || v.lastDamage <= 0 {
			t.Fatal("switch globally cleared normal immunity")
		}
	})
}

func TestWarProofOutcomesActualLossAndLethalOrder(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
		ah.overrideCrit = true
		ah.critical = true
		a.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), item.Stack{})
		_ = v.SetHealth(3)
		v.SetAbsorption(2)
		if !a.AttackEntity(v) {
			t.Fatal("lethal hit rejected")
		}
		if len(vh.outcomes) != 1 {
			t.Fatalf("outcomes=%d", len(vh.outcomes))
		}
		out := vh.outcomes[0]
		if out.HealthLost != 3 || out.AbsorptionLost != 2 || !out.Critical || !out.DirectMelee || out.TotemUsed {
			t.Fatalf("outcome=%+v", out)
		}
		if len(vh.order) != 2 || vh.order[0] != "damage" || vh.order[1] != "death" || vh.deathCalls != 1 {
			t.Fatalf("order=%v", vh.order)
		}
	})
}

func TestWarProofCritOffAndAbsorptionOnly(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
		ah.overrideCrit = true
		ah.critical = false
		a.fallDistance = 3
		a.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), item.Stack{})
		v.SetAbsorption(20)
		if !a.AttackEntity(v) {
			t.Fatal("hit rejected")
		}
		out := vh.outcomes[0]
		if out.Critical || out.HealthLost != 0 || out.AbsorptionLost <= 0 || v.Health() != 20 {
			t.Fatalf("outcome=%+v", out)
		}
	})
}

func TestWarProofMitigationProjectileAndTotem(t *testing.T) {
	t.Run("armour", func(t *testing.T) {
		proofPair(t, func(_ *world.Tx, a, v *Player, _, vh *proofHooks) {
			v.Armour().SetChestplate(item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1))
			v.Hurt(4, entity.ProjectileDamageSource{Owner: a})
			if len(vh.outcomes) != 1 {
				t.Fatal("projectile missing")
			}
			o := vh.outcomes[0]
			if o.DirectMelee || o.Critical || o.HealthLost <= 0 || o.HealthLost >= 4 || math.Abs(o.HealthLost-(20-v.Health())) > 1e-9 {
				t.Fatalf("outcome=%+v", o)
			}
		})
	})
	t.Run("totem", func(t *testing.T) {
		proofPair(t, func(_ *world.Tx, a, v *Player, _, vh *proofHooks) {
			v.SetHeldItems(item.Stack{}, item.NewStack(item.Totem{}, 1))
			v.SetAbsorption(2)
			v.Hurt(100, entity.AttackDamageSource{Attacker: a})
			o := vh.outcomes[0]
			if !o.TotemUsed || o.HealthLost != 0 || o.AbsorptionLost != 2 || v.Dead() || vh.deathCalls != 0 {
				t.Fatalf("totem outcome=%+v", o)
			}
		})
	})
}

func TestWarProofCombatStateExpiresWindowsPausesEffects(t *testing.T) {
	proofPair(t, func(tx *world.Tx, a, v *Player, _, _ *proofHooks) {
		now := time.Now()
		_ = v.SetHealth(7)
		v.SetFood(6)
		v.hunger.saturationLevel = 1.25
		v.hunger.exhaustionLevel = 2.75
		v.hunger.foodTick = 37
		v.AddEffect(effect.NewAmbient(effect.Absorption, 2, time.Second*40).WithoutParticles())
		v.AddEffect(effect.New(effect.Poison, 1, time.Second*12))
		v.SetAbsorption(1.5)
		v.directMelee = newDirectMeleeState(false)
		v.directMelee.commit(directMeleeCold, 7)
		v.immuneUntil = now.Add(time.Second / 2)
		v.lastDamage = 7
		v.cooldowns["minecraft:ender_pearl"] = now.Add(8 * time.Second)
		v.cooldowns["minecraft:snowball"] = now.Add(time.Second)
		state, err := v.captureCombatState(now)
		if err != nil {
			t.Fatal(err)
		}
		cfg := Config{UUID: v.UUID(), Health: 20, MaxHealth: 20, GameMode: world.GameModeSurvival}
		next := tx.AddEntity(world.EntitySpawnOpts{ID: v.UUID()}.New(Type, cfg)).(*Player)
		next.directMelee = newDirectMeleeState(true)
		if err := next.restoreCombatState(state, now.Add(3*time.Second)); err != nil {
			t.Fatal(err)
		}
		if next.Health() != 7 || next.Food() != 6 || next.Absorption() != 1.5 || next.hunger.foodTick != 37 || next.hunger.saturationLevel != 1.25 || next.hunger.exhaustionLevel != 2.75 {
			t.Fatal("resume refilled vitals")
		}
		for _, e := range next.Effects() {
			if e.Type() == effect.Poison && e.Duration() != 12*time.Second {
				t.Fatal("poison time changed")
			}
			if e.Type() == effect.Absorption && (e.Duration() != 40*time.Second || !e.Ambient() || !e.ParticlesHidden()) {
				t.Fatal("effect flags changed")
			}
		}
		if next.directMelee.noDamageTicks != 0 || next.directMelee.attackTime != 0 {
			t.Fatal("fresh reconnect protection survived")
		}
		if until := next.cooldowns["minecraft:ender_pearl"]; !until.Equal(now.Add(8 * time.Second)) {
			t.Fatal("cooldown restarted")
		}
		if _, ok := next.cooldowns["minecraft:snowball"]; ok {
			t.Fatal("expired cooldown revived")
		}
		if len(state.cooldowns) != 2 {
			t.Fatal("restore mutated source snapshot")
		}
		if err := next.RestoreCombatState(state); err != nil {
			t.Fatal(err)
		}
		if got := next.directMelee.noDamageTicks; got != 0 {
			t.Fatalf("resume manufactured %d protection ticks", got)
		}
		if a.RestoreCombatState(state) == nil {
			t.Fatal("another account accepted snapshot")
		}
		if next.RestoreCombatState(CombatState{}) == nil {
			t.Fatal("zero snapshot accepted")
		}
	})
}

func TestWarProofResumeShortAbsenceKeepsOnlyRemainingProtection(t *testing.T) {
	proofPair(t, func(tx *world.Tx, _, v *Player, _, _ *proofHooks) {
		now := time.Now()
		v.directMelee = directMeleeState{attackTime: 8, noDamageTicks: 10, lastBaseDamage: 7}
		s, err := v.captureCombatState(now)
		if err != nil {
			t.Fatal(err)
		}
		next := tx.AddEntity(world.EntitySpawnOpts{ID: v.UUID()}.New(Type, Config{UUID: v.UUID(), GameMode: world.GameModeSurvival})).(*Player)
		if err := next.restoreCombatState(s, now.Add(150*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if next.directMelee.attackTime != 5 || next.directMelee.noDamageTicks != 7 {
			t.Fatalf("windows=%+v", next.directMelee)
		}
	})
}

func TestWarProofHealthBoostDoesNotStackOnRestore(t *testing.T) {
	proofPair(t, func(tx *world.Tx, _, v *Player, _, _ *proofHooks) {
		v.AddEffect(effect.New(effect.HealthBoost, 2, time.Minute))
		_ = v.SetHealth(9)
		s, err := v.CaptureCombatState()
		if err != nil {
			t.Fatal(err)
		}
		next := tx.AddEntity(world.EntitySpawnOpts{ID: v.UUID()}.New(Type, Config{UUID: v.UUID(), GameMode: world.GameModeSurvival})).(*Player)
		for i := 0; i < 2; i++ {
			if err := next.RestoreCombatState(s); err != nil {
				t.Fatal(err)
			}
			if next.MaxHealth() != 28 || next.Health() != 9 {
				t.Fatalf("health/max stacked: %v/%v", next.Health(), next.MaxHealth())
			}
		}
		next.health.SetHealth(0)
		if next.RestoreCombatState(s) == nil {
			t.Fatal("dead fighter resurrected")
		}
	})
}

func TestWarProofNativePearlCooldownOverride(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, _ *Player, ah, _ *proofHooks) {
		ah.cooldown = 8 * time.Second
		a.SetHeldItems(item.NewStack(item.EnderPearl{}, 3), item.Stack{})
		before := time.Now()
		a.UseItem()
		until := a.cooldowns["minecraft:ender_pearl"]
		if until.Before(before.Add(8*time.Second)) || until.After(time.Now().Add(8*time.Second)) {
			t.Fatalf("deadline=%v", until)
		}
		main, _ := a.HeldItems()
		if main.Count() != 2 {
			t.Fatal("pearl not consumed exactly once")
		}
		a.UseItem()
		main, _ = a.HeldItems()
		if main.Count() != 2 {
			t.Fatal("active cooldown allowed another pearl")
		}
	})
}

func TestWarProofHornVariants(t *testing.T) {
	if sound.Call().Uint8() != 5 || sound.Yearn().Uint8() != 6 || sound.Dream().Uint8() != 7 {
		t.Fatal("horn mapping changed")
	}
}

type proofTrace struct {
	*proofHooks
	name  string
	trace *[]string
}

func (h *proofTrace) HandleDamageOutcome(p *Player, o DamageOutcome) {
	*h.trace = append(*h.trace, h.name+".damage")
	h.proofHooks.HandleDamageOutcome(p, o)
}
func (h *proofTrace) HandleDeath(p *Player, src world.DamageSource, keep *bool) {
	*h.trace = append(*h.trace, h.name+".death")
	h.proofHooks.HandleDeath(p, src, keep)
}

func TestWarProofPrimaryObservationPrecedesLethalThorns(t *testing.T) {
	proofPair(t, func(_ *world.Tx, a, v *Player, ah, vh *proofHooks) {
		var trace []string
		a.Handle(&proofTrace{proofHooks: ah, name: "attacker", trace: &trace})
		v.Handle(&proofTrace{proofHooks: vh, name: "victim", trace: &trace})
		a.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), item.Stack{})
		v.Armour().SetChestplate(item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.Thorns, 40)))
		_ = v.SetHealth(1)
		if !a.AttackEntity(v) {
			t.Fatal("lethal hit rejected")
		}
		want := []string{"victim.damage", "attacker.damage", "attacker.death", "victim.death"}
		if len(trace) != len(want) {
			t.Fatalf("order=%v", trace)
		}
		for i := range want {
			if trace[i] != want[i] {
				t.Fatalf("order=%v want=%v", trace, want)
			}
		}
		if !a.Dead() || !v.Dead() {
			t.Fatal("fixture did not produce simultaneous terminal batch")
		}
	})
}

func TestWarProofPrivateHornPackets(t *testing.T) {
	withStateTestPlayer(t, func(p *Player, conn *stateConn) {
		p.PlaySound(sound.GoatHorn{Horn: sound.Call()})
		p.PlaySound(sound.GoatHorn{Horn: sound.Yearn()})
		p.PlaySound(sound.GoatHorn{Horn: sound.Dream()})
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			found := map[string]bool{}
			for _, pk := range conn.snapshot() {
				if e, ok := pk.(*packet.LevelSoundEvent); ok {
					found[e.SoundType] = true
				}
			}
			if found[packet.SoundEventGoatCall5] && found[packet.SoundEventGoatCall6] && found[packet.SoundEventGoatCall7] {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("private Call/Yearn/Dream sound packets missing")
	})
}
