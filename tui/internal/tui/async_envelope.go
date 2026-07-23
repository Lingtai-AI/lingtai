package tui

import (
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

type asyncKind uint8

const (
	asyncInitialRebuild asyncKind = iota + 1
	asyncSteadyRefresh
	asyncSessionPersist
	asyncOlderPage
	asyncExactHistoryCount
	asyncRefreshTick
	asyncLivenessPulse
	asyncEditorDone
)

type asyncFieldMask uint16

const (
	asyncHasOwner asyncFieldMask = 1 << iota
	asyncHasTarget
	asyncHasGeneration
	asyncHasEpoch
	asyncHasSourceCache
	asyncHasStoreVersion
)

type asyncOwner struct {
	projectID  string
	storeID    uint64
	activation uint64
}

type asyncTarget struct {
	directory          string
	addressFingerprint string
	inventoryBound     bool
}

type asyncGeneration struct {
	thread uint64
	epoch  uint64
}

type asyncSourceCache struct {
	cache    *fs.SessionCache
	identity string
}

type asyncBinding struct {
	owner      asyncOwner
	target     asyncTarget
	generation uint64
}

type asyncEnvelope struct {
	kind         asyncKind
	fields       asyncFieldMask
	owner        asyncOwner
	target       asyncTarget
	generation   asyncGeneration
	source       asyncSourceCache
	storeVersion uint64
}

type asyncCurrent struct {
	binding          asyncBinding
	sessionSource    asyncSourceCache
	outstandingCount asyncSourceCache
	storeVersion     uint64
	tickEpoch        uint64
	pulseEpoch       uint64
	revalidateTarget func(asyncOwner, asyncTarget) bool
}

func asyncRequiredMask(kind asyncKind) (asyncFieldMask, bool) {
	base := asyncHasOwner | asyncHasTarget | asyncHasGeneration
	switch kind {
	case asyncInitialRebuild, asyncSteadyRefresh:
		return base | asyncHasStoreVersion, true
	case asyncSessionPersist, asyncOlderPage:
		return base | asyncHasSourceCache | asyncHasStoreVersion, true
	case asyncExactHistoryCount:
		return base | asyncHasSourceCache, true
	case asyncRefreshTick, asyncLivenessPulse:
		return base | asyncHasEpoch, true
	case asyncEditorDone:
		return base, true
	default:
		return 0, false
	}
}

func captureAsync(kind asyncKind, current asyncCurrent) asyncEnvelope {
	fields, known := asyncRequiredMask(kind)
	if !known {
		return asyncEnvelope{kind: kind}
	}
	envelope := asyncEnvelope{
		kind:       kind,
		fields:     fields,
		owner:      current.binding.owner,
		target:     current.binding.target,
		generation: asyncGeneration{thread: current.binding.generation},
	}
	if fields&asyncHasStoreVersion != 0 {
		envelope.storeVersion = current.storeVersion
	}
	switch kind {
	case asyncSessionPersist, asyncOlderPage:
		envelope.source = current.sessionSource
	case asyncExactHistoryCount:
		envelope.source = current.outstandingCount
	case asyncRefreshTick:
		envelope.generation.epoch = current.tickEpoch
	case asyncLivenessPulse:
		envelope.generation.epoch = current.pulseEpoch
	}
	return envelope
}

func asyncNeedsInventoryRevalidation(kind asyncKind) bool {
	switch kind {
	case asyncInitialRebuild,
		asyncSteadyRefresh,
		asyncSessionPersist,
		asyncOlderPage,
		asyncExactHistoryCount,
		asyncRefreshTick,
		asyncEditorDone:
		return true
	default:
		return false
	}
}

func acceptAsync(current asyncCurrent, got asyncEnvelope) bool {
	fields, known := asyncRequiredMask(got.kind)
	if !known || got.fields != fields {
		return false
	}

	binding := current.binding
	if !validAsyncOwner(binding.owner) || !validAsyncTarget(binding.owner, binding.target) || binding.generation == 0 ||
		got.owner != binding.owner || got.target != binding.target || got.generation.thread != binding.generation {
		return false
	}

	if fields&asyncHasEpoch == 0 {
		if got.generation.epoch != 0 {
			return false
		}
	} else {
		epoch := current.tickEpoch
		if got.kind == asyncLivenessPulse {
			epoch = current.pulseEpoch
		}
		if epoch == 0 || got.generation.epoch != epoch {
			return false
		}
	}

	if fields&asyncHasSourceCache == 0 {
		if got.source.cache != nil || got.source.identity != "" {
			return false
		}
	} else {
		switch got.kind {
		case asyncSessionPersist, asyncOlderPage:
			if !validAsyncSource(current.sessionSource) || got.source != current.sessionSource {
				return false
			}
		case asyncExactHistoryCount:
			// Count work stays bound to its originating outstanding cache, while a
			// same-horizon replacement of the installed session cache remains valid.
			if !validAsyncSource(current.outstandingCount) || !validAsyncSource(current.sessionSource) ||
				got.source != current.outstandingCount || got.source.identity != current.sessionSource.identity {
				return false
			}
		default:
			return false
		}
	}

	if fields&asyncHasStoreVersion == 0 {
		if got.storeVersion != 0 {
			return false
		}
	} else if got.storeVersion != current.storeVersion {
		// Zero is a valid version when the presence bit requires this coordinate.
		return false
	}

	// Current state owns inventory policy: captured work cannot weaken it. Pulse
	// is animation-only and deliberately avoids a four-times-per-second scan.
	if binding.target.inventoryBound && asyncNeedsInventoryRevalidation(got.kind) {
		return current.revalidateTarget != nil && current.revalidateTarget(binding.owner, binding.target)
	}
	return true
}

func validAsyncOwner(owner asyncOwner) bool {
	return owner.projectID != "" && owner.projectID == canonicalProjectMailIdentity(owner.projectID) &&
		owner.storeID != 0 && owner.activation != 0
}

func validAsyncTarget(owner asyncOwner, target asyncTarget) bool {
	if target.directory == "" || target.directory != inventory.NormalizePath(target.directory) ||
		!validAsyncAddressFingerprint(target.addressFingerprint) {
		return false
	}
	rel, err := filepath.Rel(owner.projectID, target.directory)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validAsyncAddressFingerprint(fingerprint string) bool {
	if len(fingerprint) != 64 || fingerprint == fs.AddressFingerprint("") || fingerprint != strings.ToLower(fingerprint) {
		return false
	}
	decoded, err := hex.DecodeString(fingerprint)
	return err == nil && len(decoded) == 32
}

func validAsyncSource(source asyncSourceCache) bool {
	return source.cache != nil && strings.TrimSpace(source.identity) != ""
}
