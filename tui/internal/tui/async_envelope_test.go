package tui

import (
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

type directAsyncKindCase struct {
	name string
	kind asyncKind
}

var directAsyncKindCases = []directAsyncKindCase{
	{name: "initial_rebuild", kind: asyncInitialRebuild},
	{name: "steady_refresh", kind: asyncSteadyRefresh},
	{name: "session_persist", kind: asyncSessionPersist},
	{name: "older_page", kind: asyncOlderPage},
	{name: "exact_history_count", kind: asyncExactHistoryCount},
	{name: "refresh_tick", kind: asyncRefreshTick},
	{name: "liveness_pulse", kind: asyncLivenessPulse},
	{name: "editor_done", kind: asyncEditorDone},
}

func directAsyncRequiredMask(kind asyncKind) asyncFieldMask {
	base := asyncHasOwner | asyncHasTarget | asyncHasGeneration
	switch kind {
	case asyncInitialRebuild, asyncSteadyRefresh:
		return base | asyncHasStoreVersion
	case asyncSessionPersist, asyncOlderPage:
		return base | asyncHasSourceCache | asyncHasStoreVersion
	case asyncExactHistoryCount:
		return base | asyncHasSourceCache
	case asyncRefreshTick, asyncLivenessPulse:
		return base | asyncHasEpoch
	case asyncEditorDone:
		return base
	default:
		return 0
	}
}

func directAsyncAddress(scope string) string {
	return "async-address:" + scope
}

func directAsyncCurrent(scope string, generation uint64) asyncCurrent {
	projectRoot := inventory.NormalizePath(filepath.Join("testdata", "async-envelope", scope))
	projectID := canonicalProjectMailIdentity(filepath.Join(projectRoot, ".lingtai"))
	targetDir := inventory.NormalizePath(filepath.Join(projectID, "Main"))
	source := asyncSourceCache{
		cache:    new(fs.SessionCache),
		identity: "history-horizon:" + scope,
	}
	return asyncCurrent{
		binding: asyncBinding{
			owner: asyncOwner{
				projectID:  projectID,
				storeID:    31,
				activation: 37,
			},
			target: asyncTarget{
				directory:          targetDir,
				addressFingerprint: fs.AddressFingerprint(directAsyncAddress(scope)),
			},
			generation: generation,
		},
		sessionSource:    source,
		outstandingCount: source,
		storeVersion:     41,
		tickEpoch:        43,
		pulseEpoch:       47,
	}
}

func directAssertAsyncAccepted(t *testing.T, kindName, scenario string, current asyncCurrent, got asyncEnvelope) {
	t.Helper()
	if !acceptAsync(current, got) {
		t.Errorf("kind=%s scenario=%s: acceptAsync rejected a valid current envelope; want acceptance under the eventual production predicate", kindName, scenario)
	}
}

func directAssertAsyncRejected(t *testing.T, kindName, scenario string, current asyncCurrent, got asyncEnvelope) {
	t.Helper()
	if acceptAsync(current, got) {
		t.Errorf("kind=%s scenario=%s: acceptAsync accepted stale or malformed identity; want rejection", kindName, scenario)
	}
}

func TestAsyncPredicateDirectRequiredFieldMatrix(t *testing.T) {
	for _, tc := range directAsyncKindCases {
		t.Run(tc.name, func(t *testing.T) {
			current := directAsyncCurrent(tc.name, 7)
			got := captureAsync(tc.kind, current)
			wantMask := directAsyncRequiredMask(tc.kind)
			if got.kind != tc.kind {
				t.Errorf("kind=%s scenario=capture_kind: got kind %d, want %d", tc.name, got.kind, tc.kind)
			}
			if got.fields != wantMask {
				t.Errorf("kind=%s scenario=required_field_matrix: fields=%06b want=%06b", tc.name, got.fields, wantMask)
			}
			directAssertAsyncAccepted(t, tc.name, "fully_matching_current_envelope", current, got)
		})
	}

	for _, tc := range []directAsyncKindCase{
		{name: "initial_rebuild", kind: asyncInitialRebuild},
		{name: "steady_refresh", kind: asyncSteadyRefresh},
		{name: "session_persist", kind: asyncSessionPersist},
		{name: "older_page", kind: asyncOlderPage},
	} {
		t.Run(tc.name+"_zero_store_version_present", func(t *testing.T) {
			current := directAsyncCurrent(tc.name+"-zero", 7)
			current.storeVersion = 0
			got := captureAsync(tc.kind, current)
			if got.fields&asyncHasStoreVersion == 0 || got.storeVersion != 0 {
				t.Errorf("kind=%s scenario=zero_store_version_present: fields=%06b storeVersion=%d; want presence bit set with valid value zero", tc.name, got.fields, got.storeVersion)
			}
			directAssertAsyncAccepted(t, tc.name, "zero_store_version_present", current, got)
		})
	}

	countMask := directAsyncRequiredMask(asyncExactHistoryCount)
	if countMask&asyncHasSourceCache == 0 || countMask&asyncHasStoreVersion != 0 {
		t.Errorf("kind=exact_history_count scenario=matrix_specificity: mask=%06b; want source cache plus canonical horizon and no store version", countMask)
	}
}

func TestAsyncPredicateDirectMalformedPresenceAndUnknownKind(t *testing.T) {
	knownBits := []struct {
		name string
		bit  asyncFieldMask
	}{
		{name: "owner", bit: asyncHasOwner},
		{name: "target", bit: asyncHasTarget},
		{name: "generation", bit: asyncHasGeneration},
		{name: "epoch", bit: asyncHasEpoch},
		{name: "source_cache", bit: asyncHasSourceCache},
		{name: "store_version", bit: asyncHasStoreVersion},
	}
	for _, tc := range directAsyncKindCases {
		t.Run(tc.name, func(t *testing.T) {
			current := directAsyncCurrent(tc.name, 7)
			valid := captureAsync(tc.kind, current)
			want := directAsyncRequiredMask(tc.kind)
			for _, field := range knownBits {
				malformed := valid
				if want&field.bit != 0 {
					malformed.fields &^= field.bit
					directAssertAsyncRejected(t, tc.name, "missing_required_presence_bit_"+field.name, current, malformed)
					continue
				}
				malformed.fields |= field.bit
				directAssertAsyncRejected(t, tc.name, "extra_illegal_presence_bit_"+field.name, current, malformed)
			}

			undefined := valid
			undefined.fields |= asyncFieldMask(1 << 15)
			directAssertAsyncRejected(t, tc.name, "undefined_presence_bit_15", current, undefined)
		})
	}

	current := directAsyncCurrent("unknown-kind", 7)
	unknown := captureAsync(asyncInitialRebuild, current)
	unknown.kind = asyncKind(255)
	directAssertAsyncRejected(t, "unknown_255", "kind_not_in_exact_eight", current, unknown)
}

func TestAsyncPredicateDirectGenerationABAAllKinds(t *testing.T) {
	for _, tc := range directAsyncKindCases {
		t.Run(tc.name, func(t *testing.T) {
			a7 := directAsyncCurrent("A", 7)
			oldA7 := captureAsync(tc.kind, a7)
			directAssertAsyncAccepted(t, tc.name, "A7_current", a7, oldA7)

			b8 := directAsyncCurrent("B", 8)
			directAssertAsyncRejected(t, tc.name, "A7_after_B8", b8, oldA7)

			a9 := directAsyncCurrent("A", 9)
			directAssertAsyncRejected(t, tc.name, "A7_after_return_to_A9", a9, oldA7)
			currentA9 := captureAsync(tc.kind, a9)
			directAssertAsyncAccepted(t, tc.name, "A9_current", a9, currentA9)
		})
	}
}

func TestAsyncPredicateDirectTargetAddressProjectStoreActivation(t *testing.T) {
	for _, tc := range directAsyncKindCases {
		t.Run(tc.name, func(t *testing.T) {
			base := directAsyncCurrent("identity", 7)
			got := captureAsync(tc.kind, base)

			differentTarget := base
			differentTarget.binding.target.directory = inventory.NormalizePath(filepath.Join(base.binding.owner.projectID, "Worker"))
			directAssertAsyncRejected(t, tc.name, "same_generation_different_canonical_target", differentTarget, got)

			changedAddress := base
			changedAddress.binding.target.addressFingerprint = fs.AddressFingerprint("replacement-address")
			directAssertAsyncRejected(t, tc.name, "same_directory_changed_address_fingerprint", changedAddress, got)

			changedProject := base
			changedProject.binding.owner.projectID = canonicalProjectMailIdentity(filepath.Join("testdata", "async-envelope", "other-project", ".lingtai"))
			directAssertAsyncRejected(t, tc.name, "changed_project", changedProject, got)

			changedStore := base
			changedStore.binding.owner.storeID++
			directAssertAsyncRejected(t, tc.name, "changed_store_instance", changedStore, got)

			changedActivation := base
			changedActivation.binding.owner.activation++
			directAssertAsyncRejected(t, tc.name, "changed_store_activation", changedActivation, got)
		})
	}
}

func TestAsyncPredicateDirectSourceCacheHorizonAndStoreVersion(t *testing.T) {
	for _, tc := range []directAsyncKindCase{
		{name: "session_persist", kind: asyncSessionPersist},
		{name: "older_page", kind: asyncOlderPage},
		{name: "exact_history_count", kind: asyncExactHistoryCount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := directAsyncCurrent(tc.name, 7)
			got := captureAsync(tc.kind, current)

			wrongCache := got
			wrongCache.source.cache = new(fs.SessionCache)
			directAssertAsyncRejected(t, tc.name, "stale_source_cache_instance", current, wrongCache)

			wrongHorizon := got
			wrongHorizon.source.identity += ":stale"
			directAssertAsyncRejected(t, tc.name, "stale_canonical_source_horizon", current, wrongHorizon)
		})
	}

	for _, tc := range []directAsyncKindCase{
		{name: "initial_rebuild", kind: asyncInitialRebuild},
		{name: "steady_refresh", kind: asyncSteadyRefresh},
		{name: "session_persist", kind: asyncSessionPersist},
		{name: "older_page", kind: asyncOlderPage},
	} {
		t.Run(tc.name+"_stale_store_version", func(t *testing.T) {
			current := directAsyncCurrent(tc.name, 7)
			got := captureAsync(tc.kind, current)
			advanced := current
			advanced.storeVersion++
			directAssertAsyncRejected(t, tc.name, "stale_store_version", advanced, got)
		})
	}

	t.Run("exact_history_count_same_horizon_replacement", func(t *testing.T) {
		current := directAsyncCurrent("count-replacement", 7)
		got := captureAsync(asyncExactHistoryCount, current)
		replaced := current
		replaced.sessionSource.cache = new(fs.SessionCache)
		directAssertAsyncAccepted(t, "exact_history_count", "same_horizon_current_cache_replacement", replaced, got)

		changedHorizon := replaced
		changedHorizon.sessionSource.identity += ":new"
		directAssertAsyncRejected(t, "exact_history_count", "current_canonical_horizon_changed", changedHorizon, got)
	})

	t.Run("exact_history_count_not_store_version_gated", func(t *testing.T) {
		current := directAsyncCurrent("count-version", 7)
		got := captureAsync(asyncExactHistoryCount, current)
		if got.fields&asyncHasStoreVersion != 0 {
			t.Errorf("kind=exact_history_count scenario=not_store_version_gated: capture fields=%06b unexpectedly include store version", got.fields)
		}
		advanced := current
		advanced.storeVersion++
		directAssertAsyncAccepted(t, "exact_history_count", "store_version_advanced_same_source_horizon", advanced, got)
	})
}

func TestAsyncPredicateDirectTickAndPulseExactEpochs(t *testing.T) {
	t.Run("refresh_tick", func(t *testing.T) {
		current := directAsyncCurrent("tick", 7)
		got := captureAsync(asyncRefreshTick, current)
		stale := current
		stale.tickEpoch++
		directAssertAsyncRejected(t, "refresh_tick", "stale_refresh_tick_epoch", stale, got)

		unrelated := current
		unrelated.pulseEpoch++
		directAssertAsyncAccepted(t, "refresh_tick", "unrelated_pulse_epoch_changed", unrelated, got)
	})

	t.Run("liveness_pulse", func(t *testing.T) {
		current := directAsyncCurrent("pulse", 7)
		got := captureAsync(asyncLivenessPulse, current)
		stale := current
		stale.pulseEpoch++
		directAssertAsyncRejected(t, "liveness_pulse", "stale_liveness_pulse_epoch", stale, got)

		unrelated := current
		unrelated.tickEpoch++
		directAssertAsyncAccepted(t, "liveness_pulse", "unrelated_tick_epoch_changed", unrelated, got)
	})
}

type directFakeInventoryResolver struct {
	records []inventory.Record
	calls   int
}

func (r *directFakeInventoryResolver) revalidate(owner asyncOwner, target asyncTarget) bool {
	r.calls++
	for _, record := range r.records {
		if inventory.NormalizePath(record.AgentDir) != target.directory {
			continue
		}
		recordProjectID := canonicalProjectMailIdentity(filepath.Join(record.Project, ".lingtai"))
		return recordProjectID == owner.projectID &&
			record.Enterable &&
			fs.AddressFingerprint(record.Address) == target.addressFingerprint
	}
	return false
}

func directInventoryRecord(current asyncCurrent) inventory.Record {
	return inventory.Record{
		Project:   filepath.Dir(current.binding.owner.projectID),
		AgentDir:  current.binding.target.directory,
		Address:   directAsyncAddress("inventory"),
		AgentName: "Main",
		Nickname:  "Original nickname",
		Enterable: true,
	}
}

func directAssertResolverCalls(t *testing.T, kindName, scenario string, resolver *directFakeInventoryResolver, want int) {
	t.Helper()
	if resolver.calls != want {
		t.Errorf("kind=%s scenario=%s: inventory resolver calls=%d want=%d", kindName, scenario, resolver.calls, want)
	}
}

func TestAsyncPredicateDirectInventoryRevalidation(t *testing.T) {
	inventoryKinds := []directAsyncKindCase{
		{name: "initial_rebuild", kind: asyncInitialRebuild},
		{name: "steady_refresh", kind: asyncSteadyRefresh},
		{name: "session_persist", kind: asyncSessionPersist},
		{name: "older_page", kind: asyncOlderPage},
		{name: "exact_history_count", kind: asyncExactHistoryCount},
		{name: "refresh_tick", kind: asyncRefreshTick},
		{name: "editor_done", kind: asyncEditorDone},
	}

	for _, tc := range inventoryKinds {
		t.Run(tc.name, func(t *testing.T) {
			base := directAsyncCurrent("inventory", 7)
			base.binding.target.inventoryBound = true
			exactRecord := directInventoryRecord(base)

			exactResolver := &directFakeInventoryResolver{records: []inventory.Record{exactRecord}}
			exact := base
			exact.revalidateTarget = exactResolver.revalidate
			directAssertAsyncAccepted(t, tc.name, "inventory_exact_match", exact, captureAsync(tc.kind, exact))
			directAssertResolverCalls(t, tc.name, "inventory_exact_match", exactResolver, 1)

			missingResolver := &directFakeInventoryResolver{}
			missing := base
			missing.revalidateTarget = missingResolver.revalidate
			directAssertAsyncRejected(t, tc.name, "inventory_record_missing", missing, captureAsync(tc.kind, missing))
			directAssertResolverCalls(t, tc.name, "inventory_record_missing", missingResolver, 1)

			wrongProjectRecord := exactRecord
			wrongProjectRecord.Project = inventory.NormalizePath(filepath.Join("testdata", "async-envelope", "wrong-project"))
			wrongProjectResolver := &directFakeInventoryResolver{records: []inventory.Record{wrongProjectRecord}}
			wrongProject := base
			wrongProject.revalidateTarget = wrongProjectResolver.revalidate
			directAssertAsyncRejected(t, tc.name, "inventory_wrong_project", wrongProject, captureAsync(tc.kind, wrongProject))
			directAssertResolverCalls(t, tc.name, "inventory_wrong_project", wrongProjectResolver, 1)

			ineligibleRecord := exactRecord
			ineligibleRecord.Enterable = false
			ineligibleResolver := &directFakeInventoryResolver{records: []inventory.Record{ineligibleRecord}}
			ineligible := base
			ineligible.revalidateTarget = ineligibleResolver.revalidate
			directAssertAsyncRejected(t, tc.name, "inventory_record_ineligible", ineligible, captureAsync(tc.kind, ineligible))
			directAssertResolverCalls(t, tc.name, "inventory_record_ineligible", ineligibleResolver, 1)

			addressChangedRecord := exactRecord
			addressChangedRecord.Address = "replacement-inventory-address"
			addressChangedResolver := &directFakeInventoryResolver{records: []inventory.Record{addressChangedRecord}}
			addressChanged := base
			addressChanged.revalidateTarget = addressChangedResolver.revalidate
			directAssertAsyncRejected(t, tc.name, "inventory_address_changed", addressChanged, captureAsync(tc.kind, addressChanged))
			directAssertResolverCalls(t, tc.name, "inventory_address_changed", addressChangedResolver, 1)

			nicknameChangedRecord := exactRecord
			nicknameChangedRecord.Nickname = "Display-only rename"
			nicknameChangedResolver := &directFakeInventoryResolver{records: []inventory.Record{nicknameChangedRecord}}
			nicknameChanged := base
			nicknameChanged.revalidateTarget = nicknameChangedResolver.revalidate
			directAssertAsyncAccepted(t, tc.name, "inventory_nickname_only_changed", nicknameChanged, captureAsync(tc.kind, nicknameChanged))
			directAssertResolverCalls(t, tc.name, "inventory_nickname_only_changed", nicknameChangedResolver, 1)
		})
	}

	t.Run("liveness_pulse_skips_process_inventory", func(t *testing.T) {
		current := directAsyncCurrent("inventory", 7)
		current.binding.target.inventoryBound = true
		resolver := &directFakeInventoryResolver{}
		current.revalidateTarget = resolver.revalidate
		directAssertAsyncAccepted(t, "liveness_pulse", "animation_only_pulse_uses_binding_not_process_scan", current, captureAsync(asyncLivenessPulse, current))
		directAssertResolverCalls(t, "liveness_pulse", "animation_only_pulse_uses_binding_not_process_scan", resolver, 0)
	})
}

func TestAsyncPredicateDirectNonInventoryMainAndCurrentPolicy(t *testing.T) {
	for _, tc := range directAsyncKindCases {
		t.Run(tc.name, func(t *testing.T) {
			current := directAsyncCurrent("stopped-main", 7)
			current.binding.target.inventoryBound = false
			resolver := &directFakeInventoryResolver{}
			current.revalidateTarget = resolver.revalidate
			got := captureAsync(tc.kind, current)

			directAssertAsyncAccepted(t, tc.name, "non_inventory_bound_stopped_main_missing_live_record", current, got)
			directAssertResolverCalls(t, tc.name, "non_inventory_bound_stopped_main_missing_live_record", resolver, 0)

			wrongDirectory := got
			wrongDirectory.target.directory = inventory.NormalizePath(filepath.Join(current.binding.owner.projectID, "DifferentMain"))
			directAssertAsyncRejected(t, tc.name, "non_inventory_main_wrong_canonical_directory", current, wrongDirectory)

			wrongAddress := got
			wrongAddress.target.addressFingerprint = fs.AddressFingerprint("different-main-address")
			directAssertAsyncRejected(t, tc.name, "non_inventory_main_wrong_address_fingerprint", current, wrongAddress)
		})
	}

	t.Run("captured_work_cannot_weaken_current_inventory_policy", func(t *testing.T) {
		current := directAsyncCurrent("inventory", 7)
		current.binding.target.inventoryBound = true
		resolver := &directFakeInventoryResolver{}
		current.revalidateTarget = resolver.revalidate
		got := captureAsync(asyncEditorDone, current)
		got.target.inventoryBound = false
		directAssertAsyncRejected(t, "editor_done", "captured_inventory_bound_false_cannot_bypass_current_bound_policy", current, got)
	})
}
