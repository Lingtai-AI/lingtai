package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func directPagingMarker(index int) string {
	return fmt.Sprintf("DIRECT-PAGING-%02d", index)
}

func directPagingBody(index int) string {
	return directPagingMarker(index) + " bounded direct paging body"
}

func directPagingMessages(target fs.DirectTarget, count int) []fs.MailMessage {
	messages := make([]fs.MailMessage, 0, count)
	for index := range count {
		messages = append(messages, directPerformanceIncoming(target, index, directPagingBody(index)))
	}
	return messages
}

func directPagingMarkers(projection []ChatMessage) []string {
	markers := make([]string, 0, len(projection))
	for _, message := range projection {
		marker, _, _ := strings.Cut(message.Body, " ")
		markers = append(markers, marker)
	}
	return markers
}

func directPagingExpectedMarkers(first, last int) []string {
	markers := make([]string, 0, last-first+1)
	for index := first; index <= last; index++ {
		markers = append(markers, directPagingMarker(index))
	}
	return markers
}

func directPagingProjectionSummary(mail MailModel) string {
	return fmt.Sprintf("projection=%d horizon=%d hasOlder=%v markers=%v",
		len(mail.directChat.projection),
		mail.directChat.revealHorizon,
		mail.directChat.hasOlder,
		directPagingMarkers(mail.directChat.projection),
	)
}

func directPagingAssertProjection(t *testing.T, mail MailModel, wantFirst, wantLast, wantHorizon int, wantHasOlder bool, context string) {
	t.Helper()
	wantMarkers := directPagingExpectedMarkers(wantFirst, wantLast)
	gotMarkers := directPagingMarkers(mail.directChat.projection)
	if len(mail.directChat.projection) != len(wantMarkers) ||
		mail.directChat.revealHorizon != wantHorizon ||
		mail.directChat.hasOlder != wantHasOlder ||
		!reflect.DeepEqual(gotMarkers, wantMarkers) {
		t.Fatalf("%s projection/horizon/markers = %s; want projection=%d horizon=%d hasOlder=%v markers=%v",
			context,
			directPagingProjectionSummary(mail),
			len(wantMarkers),
			wantHorizon,
			wantHasOlder,
			wantMarkers,
		)
	}
}

// directPagingOpenAgents follows the public palette command into Mail's actual
// /agents selector. A preserved non-empty Main draft cannot be typed over with
// "/agents", so it dispatches the exact PaletteSelectMsg the real palette emits;
// target activation still travels only through selector keys and Enter.
func directPagingOpenAgents(t *testing.T, app App) App {
	t.Helper()
	if app.mail.agentSelector.selectorOpen {
		t.Fatal("precondition: /agents selector is already open")
	}
	if app.mail.input.Value() == "" {
		for _, r := range "/agents" {
			app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		var paletteCmd tea.Cmd
		app, paletteCmd = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
		if paletteCmd == nil {
			t.Fatal("typing /agents produced no palette selection command")
		}
		selected, ok := paletteCmd().(PaletteSelectMsg)
		if !ok || selected.Command != "agents" {
			t.Fatalf("typing /agents produced %#v, want PaletteSelectMsg for agents", selected)
		}
		app, _ = directPerformanceApply(app, selected)
	} else {
		app, _ = directPerformanceApply(app, PaletteSelectMsg{Command: "agents"})
	}
	if !app.mail.agentSelector.selectorOpen {
		t.Fatal("real /agents route did not open the Mail-owned selector")
	}
	return app
}

func directPagingSelectThroughAgents(t *testing.T, app App, agentID string) (App, tea.Cmd) {
	t.Helper()
	app = directPagingOpenAgents(t, app)
	index := -1
	for candidate, row := range app.mail.agentSelector.rows {
		if agentID == "" && row.Main || !row.Main && row.Target.AgentID == agentID {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatalf("/agents selector did not publish agent %q", agentID)
	}
	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
	for range index {
		app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	var cmd tea.Cmd
	app, cmd = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.mail.agentSelector.selectorOpen {
		t.Fatal("/agents Enter left the selector open")
	}
	if agentID == "" {
		if _, direct := app.mail.currentDirectTarget(); direct {
			t.Fatal("/agents Main activation retained a direct target")
		}
	} else if target, direct := app.mail.currentDirectTarget(); !direct || target.AgentID != agentID {
		t.Fatalf("/agents activation current target = %#v (current=%v), want %q", target, direct, agentID)
	}
	return app, cmd
}

// newDirectPagingFixture reuses the safe accepted in-memory performance fixture,
// then narrows only the test model's direct page setting to the compact ten-row
// contract fixture. No target, projection, cache, or accepted snapshot is assigned.
func newDirectPagingFixture(t *testing.T, pageSize, width, height int, accepted []fs.MailMessage) directPerformanceFixture {
	t.Helper()
	fixture := newDirectPerformanceFixture(t, pageSize, width, height, accepted)
	fixture.app.mail.pageSize = pageSize
	return fixture
}

func directPagingRefresh(t *testing.T, app App, accepted []fs.MailMessage) (App, tea.Cmd) {
	t.Helper()
	return directPerformancePreparedRefresh(t, app, accepted)
}

// TestDirectOlderPagingRevealsOneCurrentOnlyPageAtATime defines direct Ctrl+U
// as an explicit, synchronous in-memory reveal of exactly one accepted page.
// It deliberately drives target entry and return through the public /agents
// palette and selector instead of assigning current direct state.
func TestDirectOlderPagingRevealsOneCurrentOnlyPageAtATime(t *testing.T) {
	const (
		pageSize     = 10
		messageCount = 25
	)

	target := fs.DirectTarget{AgentID: "agent-a", Address: "performance/agent-a"}
	accepted := directPagingMessages(target, messageCount)
	fixture := newDirectPagingFixture(t, pageSize, 80, 24, accepted)

	mainContentBefore := fixture.app.mail.viewport.GetContent()
	mainOffsetBefore := fixture.app.mail.viewport.YOffset()
	mainInputBefore := fixture.app.mail.input.Value()
	app := directPerformanceActivateA(t, fixture.app, fixture.targetA)

	directPagingAssertProjection(t, app.mail, 15, 24, pageSize, true, "initial /agents A activation")
	if got := len(app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)); got != messageCount {
		t.Fatalf("initial /agents A activation full accepted snapshot = %d messages; want %d", got, messageCount)
	}

	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
	if !app.mail.directChat.viewport.AtTop() {
		t.Fatal("Home did not put the stored direct viewport at its top before Ctrl+U")
	}
	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	directPagingAssertProjection(t, app.mail, 5, 24, 2*pageSize, true, "first top Ctrl+U")
	if !app.mail.directChat.viewport.AtTop() {
		t.Fatal("first top Ctrl+U did not retain the stored direct viewport top anchor")
	}
	if got := len(app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)); got != messageCount {
		t.Fatalf("first top Ctrl+U full accepted snapshot = %d messages; want %d", got, messageCount)
	}

	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	directPagingAssertProjection(t, app.mail, 0, 24, 3*pageSize, false, "second top Ctrl+U")
	if !app.mail.directChat.viewport.AtTop() {
		t.Fatal("second top Ctrl+U did not retain the stored direct viewport top anchor")
	}
	if got := len(app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)); got != messageCount {
		t.Fatalf("second top Ctrl+U full accepted snapshot = %d messages; want %d", got, messageCount)
	}

	projectionBeforeThird := append([]ChatMessage(nil), app.mail.directChat.projection...)
	horizonBeforeThird := app.mail.directChat.revealHorizon
	contentBeforeThird := app.mail.directChat.viewport.GetContent()
	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if !reflect.DeepEqual(app.mail.directChat.projection, projectionBeforeThird) ||
		app.mail.directChat.revealHorizon != horizonBeforeThird ||
		app.mail.directChat.viewport.GetContent() != contentBeforeThird {
		t.Fatalf("third top Ctrl+U was not idempotent: before projection=%v horizon=%d content=%q; after %s content=%q",
			directPagingMarkers(projectionBeforeThird),
			horizonBeforeThird,
			contentBeforeThird,
			directPagingProjectionSummary(app.mail),
			app.mail.directChat.viewport.GetContent(),
		)
	}

	app, returnCmd := directPagingSelectThroughAgents(t, app, "")
	if returnCmd != nil {
		t.Fatal("real /agents Main return produced a direct visibility command")
	}
	if app.mail.viewport.GetContent() != mainContentBefore || app.mail.viewport.YOffset() != mainOffsetBefore ||
		app.mail.input.Value() != mainInputBefore {
		t.Fatalf("real /agents Main return replaced Main state: contentEqual=%v offset=%d want=%d compose=%q want=%q",
			app.mail.viewport.GetContent() == mainContentBefore,
			app.mail.viewport.YOffset(),
			mainOffsetBefore,
			app.mail.input.Value(),
			mainInputBefore,
		)
	}

	const mainDraft = "MAIN-DRAFT-MUST-OUTLIVE-DIRECT-PAGING"
	app.mail.input.SetValue(mainDraft)
	app.mail.syncViewportHeight()
	mainContentBeforeReentry := app.mail.viewport.GetContent()
	mainOffsetBeforeReentry := app.mail.viewport.YOffset()
	app, _ = directPagingSelectThroughAgents(t, app, fixture.targetA.AgentID)
	directPagingAssertProjection(t, app.mail, 15, 24, pageSize, true, "A re-entry through /agents")
	if app.mail.directChat.mainInput.Value() != mainDraft ||
		app.mail.viewport.GetContent() != mainContentBeforeReentry ||
		app.mail.viewport.YOffset() != mainOffsetBeforeReentry {
		t.Fatalf("A re-entry replaced stored Main compose/viewport: storedCompose=%q want=%q contentEqual=%v offset=%d want=%d",
			app.mail.directChat.mainInput.Value(),
			mainDraft,
			app.mail.viewport.GetContent() == mainContentBeforeReentry,
			app.mail.viewport.YOffset(),
			mainOffsetBeforeReentry,
		)
	}
}

// TestDirectStoredViewportInvalidationIsContentAndWidthAffine characterizes the
// accepted direct viewport publication boundary: identical pages and height-only
// resizes retain stored bytes, while width or bounded-page changes republish once.
func TestDirectStoredViewportInvalidationIsContentAndWidthAffine(t *testing.T) {
	const (
		pageSize     = 10
		messageCount = 25
	)

	target := fs.DirectTarget{AgentID: "agent-a", Address: "performance/agent-a"}
	accepted := directPagingMessages(target, messageCount)
	fixture := newDirectPagingFixture(t, pageSize, 72, 24, accepted)
	app := directPerformanceActivateA(t, fixture.app, fixture.targetA)
	directPagingAssertProjection(t, app.mail, 15, 24, pageSize, true, "bounded current page precondition")

	const sentinelOffset = 3
	sentinel := strings.Repeat("DIRECT-STORED-VIEWPORT-SENTINEL\n", 32)
	app.mail.directChat.viewport.SetContent(sentinel)
	app.mail.directChat.viewport.SetYOffset(sentinelOffset)
	if app.mail.directChat.viewport.GetContent() != sentinel || app.mail.directChat.viewport.YOffset() != sentinelOffset {
		t.Fatalf("sentinel precondition content/offset = %q/%d; want exact sentinel/%d", app.mail.directChat.viewport.GetContent(), app.mail.directChat.viewport.YOffset(), sentinelOffset)
	}
	acceptedSerialBefore := app.mail.acceptedSnapshotSerial
	app, _ = directPagingRefresh(t, app, accepted)
	if app.mail.acceptedSnapshotSerial != acceptedSerialBefore+1 ||
		app.mail.directChat.acceptedSnapshotSerial != app.mail.acceptedSnapshotSerial {
		t.Fatalf("byte-identical accepted refresh coordinates = accepted=%d direct=%d; want accepted=%d direct=%d",
			app.mail.acceptedSnapshotSerial,
			app.mail.directChat.acceptedSnapshotSerial,
			acceptedSerialBefore+1,
			acceptedSerialBefore+1,
		)
	}
	if app.mail.directChat.viewport.GetContent() != sentinel || app.mail.directChat.viewport.YOffset() != sentinelOffset {
		t.Fatalf("byte-identical bounded refresh replaced stored viewport: contentEqual=%v offset=%d want=%d",
			app.mail.directChat.viewport.GetContent() == sentinel,
			app.mail.directChat.viewport.YOffset(),
			sentinelOffset,
		)
	}

	childWidthBeforeHeightChange := app.mail.width
	viewportHeightBeforeHeightChange := app.mail.directChat.viewport.Height()
	app, _ = directPerformanceApply(app, tea.WindowSizeMsg{Width: childWidthBeforeHeightChange, Height: 30})
	if app.mail.width != childWidthBeforeHeightChange || app.mail.directChat.viewport.Height() == viewportHeightBeforeHeightChange {
		t.Fatalf("height-only resize child width/viewport height = %d/%d; want width=%d and a changed height from %d",
			app.mail.width,
			app.mail.directChat.viewport.Height(),
			childWidthBeforeHeightChange,
			viewportHeightBeforeHeightChange,
		)
	}
	if app.mail.directChat.viewport.GetContent() != sentinel || app.mail.directChat.viewport.YOffset() != sentinelOffset {
		t.Fatalf("height-only resize replaced stored viewport: contentEqual=%v offset=%d want=%d",
			app.mail.directChat.viewport.GetContent() == sentinel,
			app.mail.directChat.viewport.YOffset(),
			sentinelOffset,
		)
	}

	projectionBeforeWidthChange := append([]ChatMessage(nil), app.mail.directChat.projection...)
	app, _ = directPerformanceApply(app, tea.WindowSizeMsg{Width: childWidthBeforeHeightChange + 16, Height: 30})
	expectedReflow := app.mail.renderMessages(app.mail.directChat.projection)
	if app.mail.directChat.renderWidth != app.mail.width ||
		!reflect.DeepEqual(app.mail.directChat.projection, projectionBeforeWidthChange) ||
		app.mail.directChat.viewport.GetContent() != expectedReflow ||
		app.mail.directChat.viewport.YOffset() != sentinelOffset {
		t.Fatalf("child-width resize invalidation = renderWidth=%d childWidth=%d projection=%v contentEqual=%v offset=%d want=%d",
			app.mail.directChat.renderWidth,
			app.mail.width,
			directPagingMarkers(app.mail.directChat.projection),
			app.mail.directChat.viewport.GetContent() == expectedReflow,
			app.mail.directChat.viewport.YOffset(),
			sentinelOffset,
		)
	}

	changedAccepted := append(append([]fs.MailMessage(nil), accepted...), directPerformanceIncoming(fixture.targetA, messageCount, directPagingBody(messageCount)))
	contentBeforeChangedPage := app.mail.directChat.viewport.GetContent()
	app, _ = directPagingRefresh(t, app, changedAccepted)
	expectedChangedPage := app.mail.renderMessages(app.mail.directChat.projection)
	directPagingAssertProjection(t, app.mail, 16, 25, pageSize, true, "changed newest accepted page")
	if app.mail.directChat.viewport.GetContent() != expectedChangedPage ||
		app.mail.directChat.viewport.GetContent() == contentBeforeChangedPage ||
		!app.mail.directChat.viewport.AtBottom() {
		t.Fatalf("changed bounded page publication content/tail = contentEqual=%v changed=%v atBottom=%v markers=%v",
			app.mail.directChat.viewport.GetContent() == expectedChangedPage,
			app.mail.directChat.viewport.GetContent() != contentBeforeChangedPage,
			app.mail.directChat.viewport.AtBottom(),
			directPagingMarkers(app.mail.directChat.projection),
		)
	}
	if got := len(app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)); got != messageCount+1 {
		t.Fatalf("changed bounded page full accepted snapshot = %d messages; want %d", got, messageCount+1)
	}
}
