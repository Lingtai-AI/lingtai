package tui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	reviewSparseSmallUnrelated = 4
	reviewSparseLargeUnrelated = 20004
	reviewSparsePageSize       = 10
	reviewSparseDirectMessages = 2*reviewSparsePageSize + 1
	reviewSparseAllocsRuns     = 3
	reviewSparseAllowedDelta   = 512.0
)

type reviewSparseFixture struct {
	mail   MailModel
	target fs.DirectTarget
}

// newReviewSparseFixture builds and accepts the real refresh publication before
// any allocation measurement. The sparse prefix is older than the fixed direct
// tail, and every unrelated message has a recipient slice so a hidden full-cache
// clone has a deterministic allocation cost.
func newReviewSparseFixture(t *testing.T, unrelated int) reviewSparseFixture {
	t.Helper()
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtaiDir, "human")
	target := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtaiDir, "agent-a"),
		AgentID:          "agent-a",
		Address:          "review-sparse/agent-a",
	}
	directPerformanceWriteManifest(t, humanDir, "human", "Human", directPerformanceHuman, true)
	directPerformanceWriteManifest(t, target.Directory, target.AgentID, "Alpha", target.Address, false)

	accepted := make([]fs.MailMessage, 0, unrelated+reviewSparseDirectMessages)
	oldest := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	for index := range unrelated {
		accepted = append(accepted, fs.MailMessage{
			MailboxID:  fmt.Sprintf("review-sparse-unrelated-%05d", index),
			From:       "review-sparse/unrelated-sender",
			To:         []string{"review-sparse/unrelated-recipient"},
			Message:    "unrelated sparse hidden message",
			ReceivedAt: oldest.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			Delivered:  true,
		})
	}
	for index := range reviewSparseDirectMessages {
		accepted = append(accepted, directPerformanceIncoming(target, index, fmt.Sprintf("review direct %02d", index)))
	}

	mail := NewMailModel(
		humanDir,
		directPerformanceHuman,
		lingtaiDir,
		"",
		"Main",
		reviewSparsePageSize,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 73
	mail.cache = fs.NewMailCache(humanDir)
	mail.cache.Messages = accepted
	mail, _ = mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// NewMailModel clamps production settings below the supported minimum; this
	// focused paging fixture uses the existing test seam to keep the bounded page
	// small while leaving the accepted publication untouched.
	mail.pageSize = reviewSparsePageSize
	msg := reviewBlockerRealRefresh(t, mail)
	mail, _ = mail.Update(msg)

	if !mail.acceptedSnapshot.ready {
		t.Fatal("real refresh did not install an accepted publication")
	}
	if got, want := len(mail.acceptedSnapshot.cache.Messages), len(accepted); got != want {
		t.Fatalf("accepted publication messages = %d, want %d", got, want)
	}
	return reviewSparseFixture{mail: mail, target: target}
}

func reviewSparseActivationAllocs(mail MailModel, target fs.DirectTarget) float64 {
	var retained MailModel
	allocs := testing.AllocsPerRun(reviewSparseAllocsRuns, func() {
		retained, _ = mail.activateDirectTarget(target)
	})
	_ = retained.directChat.revealHorizon
	return allocs
}

func reviewSparsePagingModel(t *testing.T, fixture reviewSparseFixture) MailModel {
	t.Helper()
	mail, cmd := fixture.mail.activateDirectTarget(fixture.target)
	if cmd == nil {
		t.Fatal("direct activation produced no deferred visibility command")
	}
	if !mail.directChat.hasOlder || mail.directChat.revealHorizon != reviewSparsePageSize {
		t.Fatalf("direct activation paging precondition = horizon %d hasOlder %v, want %d/true", mail.directChat.revealHorizon, mail.directChat.hasOlder, reviewSparsePageSize)
	}
	mail.directChat.viewport.GotoTop()
	if !mail.directChat.viewport.AtTop() {
		t.Fatal("could not establish top anchor before measured Ctrl+U reveal")
	}
	probe, _ := mail.updateDirectScroll(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if probe.directChat.revealHorizon != 2*reviewSparsePageSize || len(probe.directChat.projection) != 2*reviewSparsePageSize {
		t.Fatalf("one top Ctrl+U probe = horizon %d projection %d, want %d/%d", probe.directChat.revealHorizon, len(probe.directChat.projection), 2*reviewSparsePageSize, 2*reviewSparsePageSize)
	}
	return mail
}

func reviewSparsePagingAllocs(mail MailModel) float64 {
	key := tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	var retained MailModel
	allocs := testing.AllocsPerRun(reviewSparseAllocsRuns, func() {
		retained, _ = mail.updateDirectScroll(key)
	})
	_ = retained.directChat.revealHorizon
	return allocs
}

func TestDirectActivationAllocationsDoNotScaleWithSparseHiddenHistory(t *testing.T) {
	small := newReviewSparseFixture(t, reviewSparseSmallUnrelated)
	large := newReviewSparseFixture(t, reviewSparseLargeUnrelated)

	smallAllocs := reviewSparseActivationAllocs(small.mail, small.target)
	largeAllocs := reviewSparseActivationAllocs(large.mail, large.target)
	t.Logf("direct activation allocations: small=%0.0f sparse-large=%0.0f allowed-delta=%0.0f", smallAllocs, largeAllocs, reviewSparseAllowedDelta)
	if largeAllocs > smallAllocs+reviewSparseAllowedDelta {
		t.Errorf("direct activation allocations scale with ~20,000 unrelated sparse hidden messages: small=%0.0f sparse-large=%0.0f delta=%0.0f, want delta <= %0.0f", smallAllocs, largeAllocs, largeAllocs-smallAllocs, reviewSparseAllowedDelta)
	}
}

func TestDirectTopCtrlUAllocationsDoNotScaleWithSparseHiddenHistory(t *testing.T) {
	smallFixture := newReviewSparseFixture(t, reviewSparseSmallUnrelated)
	largeFixture := newReviewSparseFixture(t, reviewSparseLargeUnrelated)
	small := reviewSparsePagingModel(t, smallFixture)
	large := reviewSparsePagingModel(t, largeFixture)

	smallAllocs := reviewSparsePagingAllocs(small)
	largeAllocs := reviewSparsePagingAllocs(large)
	t.Logf("top-anchored Ctrl+U allocations: small=%0.0f sparse-large=%0.0f allowed-delta=%0.0f", smallAllocs, largeAllocs, reviewSparseAllowedDelta)
	if largeAllocs > smallAllocs+reviewSparseAllowedDelta {
		t.Errorf("one top-anchored Ctrl+U page reveal allocations scale with ~20,000 unrelated sparse hidden messages: small=%0.0f sparse-large=%0.0f delta=%0.0f, want delta <= %0.0f", smallAllocs, largeAllocs, largeAllocs-smallAllocs, reviewSparseAllowedDelta)
	}
}
