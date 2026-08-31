package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/streamprogress"
)

// Stream-progress badge — the Mail-side adapter of the kernel's RAM-resident
// stream-progress API (internal/streamprogress). While the visible recipient
// is ACTIVE and its agent process reports an active provider response, the
// "Email To:" footer indicator gains a factual char count plus source-snapshot
// freshness beside `Active N s`:
//
//	◉ active 12s · 1.0k chars · delay 0.1s
//
// `streamed_chars` is displayed directly (one decimal k above 999). `delay` is
// max(now - updated_unix_ms, 0), not provider-response duration.
//
// Ownership and gating:
//   - All I/O happens in fetchStreamProgressCmd, a tea.Cmd scheduled from the
//     poll tick through maybeScheduleStreamProgress (one in-flight fetch per
//     activation generation + recipient, bounded by a TTL). View() only reads
//     the cached streamProgressState.
//   - Every scheduled fetch carries a monotonic request serial. Only the
//     newest serial for the current Mail generation may land: a TTL-expired
//     request that reports after its replacement, or a previous recipient's
//     request after a switch, is a no-op — it neither clears the newer
//     request's in-flight gate nor touches the cached reading.
//   - A result is accepted only when its Mail generation still matches AND its
//     recipient identity (stable agent_id + directory, plus the direct thread
//     key) is exactly the currently visible recipient — Main's orchestrator or
//     the validated direct target. Anything else is dropped without effect.
//   - The suffix renders only while that recipient's lifecycle is ACTIVE and
//     the snapshot itself is active; it clears immediately when the snapshot
//     is inactive/unavailable/mismatched or the visible recipient changes.
//   - Nothing is persisted; the kernel publishes no text and this side reads
//     none.

// streamProgressFetcher is the consumer-side seam over
// streamprogress.Client.Fetch so Mail tests can drive the badge with an
// in-memory fake and prove no network I/O ever happens on the render path.
type streamProgressFetcher interface {
	Fetch(agentID string) (streamprogress.Snapshot, bool)
}

// streamRecipient is the identity a fetch was issued for. Main leaves threadKey
// empty; a direct page carries the validated target's stable thread key.
type streamRecipient struct {
	agentID   string
	directory string
	threadKey string
}

// streamProgressMsg is the async fetch result, tagged with the activation
// generation, the request serial, and the recipient it was issued for.
type streamProgressMsg struct {
	generation uint64
	serial     uint64
	recipient  streamRecipient
	snapshot   streamprogress.Snapshot
	ok         bool
}

// streamProgressState is the one cached reading View() consumes.
type streamProgressState struct {
	recipient streamRecipient
	snapshot  streamprogress.Snapshot
	ok        bool
}

// streamProgressInFlightTTL bounds a fetch that never reported back (its
// message was dropped by a view switch or a generation change) so polling
// resumes instead of wedging on a stale in-flight flag. The replacement gets
// a newer serial, so the expired request can no longer land.
const streamProgressInFlightTTL = 3 * time.Second

// visibleStreamRecipient resolves the identity of the currently visible
// recipient: the validated direct target when a direct page is current,
// otherwise Main's orchestrator.
func (m MailModel) visibleStreamRecipient() streamRecipient {
	if target, ok := m.currentDirectTarget(); ok {
		return streamRecipient{agentID: target.AgentID, directory: target.Directory, threadKey: m.directChat.threadKey}
	}
	return streamRecipient{agentID: m.orchAgentID, directory: m.orchestrator}
}

// visibleRecipientActive reports whether the visible recipient's lifecycle
// presentation is ACTIVE — the only state in which progress may show.
func (m MailModel) visibleRecipientActive() bool {
	return strings.EqualFold(m.activeRecipientLifecycle().state, "ACTIVE")
}

// maybeScheduleStreamProgress returns a fetch command when a poll is warranted
// or nil otherwise. It is the single gate: the poll tick funnels through it, so
// no other path spawns stream-progress I/O. It mutates only the in-flight
// bookkeeping and clears the cached reading when the recipient is not ACTIVE
// or changed; a fresh reading lands only via streamProgressMsg.
//
// The in-flight gate suppresses a poll only for the same activation
// generation, the same visible recipient, and an unexpired request; a new
// generation, a recipient switch, or TTL expiry schedules at once, and each
// schedule hands out the next serial so any earlier request becomes a no-op.
func (m *MailModel) maybeScheduleStreamProgress(now time.Time) tea.Cmd {
	recipient := m.visibleStreamRecipient()
	if !m.visibleRecipientActive() || recipient.agentID == "" {
		m.streamProgress = streamProgressState{}
		return nil
	}
	if m.streamProgress.ok && m.streamProgress.recipient != recipient {
		m.streamProgress = streamProgressState{}
	}
	if m.streamProgressFetcher == nil {
		return nil
	}
	if m.streamProgressInFlight && m.streamProgressInFlightGen == m.generation &&
		m.streamProgressInFlightFor == recipient &&
		now.Sub(m.streamProgressInFlightAt) < streamProgressInFlightTTL {
		return nil
	}
	m.streamProgressSerial++
	m.streamProgressInFlight = true
	m.streamProgressInFlightGen = m.generation
	m.streamProgressInFlightFor = recipient
	m.streamProgressInFlightAt = now
	return fetchStreamProgressCmd(m.streamProgressFetcher, m.generation, m.streamProgressSerial, recipient)
}

// fetchStreamProgressCmd performs the one loopback read off the Bubble Tea
// loop and reports it tagged with the generation/serial/recipient it was
// issued for.
func fetchStreamProgressCmd(fetcher streamProgressFetcher, generation, serial uint64, recipient streamRecipient) tea.Cmd {
	return func() tea.Msg {
		snapshot, ok := fetcher.Fetch(recipient.agentID)
		return streamProgressMsg{generation: generation, serial: serial, recipient: recipient, snapshot: snapshot, ok: ok}
	}
}

// applyStreamProgress lands an async result. A stale generation or any serial
// other than the newest scheduled one is ignored entirely — it must not clear
// the newer request's gate or overwrite its reading. For the newest request,
// a result for any recipient other than the currently visible one, an
// unavailable endpoint, or an inactive snapshot clears the badge.
func (m *MailModel) applyStreamProgress(msg streamProgressMsg) {
	if msg.generation != m.generation || msg.serial != m.streamProgressSerial {
		return
	}
	m.streamProgressInFlight = false
	if msg.recipient != m.visibleStreamRecipient() || !m.visibleRecipientActive() || !msg.ok || !msg.snapshot.Active {
		m.streamProgress = streamProgressState{}
		return
	}
	m.streamProgress = streamProgressState{recipient: msg.recipient, snapshot: msg.snapshot, ok: true}
}

// formatStreamedChars preserves the factual API unit. Values below 1000 stay
// exact; values at or above 1000 use one decimal k for the narrow footer.
func formatStreamedChars(chars int64) string {
	if chars < 0 {
		chars = 0
	}
	if chars < 1000 {
		return fmt.Sprintf("%d", chars)
	}
	return fmt.Sprintf("%.1fk", float64(chars)/1000)
}

// streamProgressDelaySeconds reports source-snapshot freshness, not provider
// duration. Clamp future timestamps to zero so clock skew cannot show a
// negative delay.
func streamProgressDelaySeconds(updatedUnixMS int64, now time.Time) float64 {
	delayMS := now.UnixMilli() - updatedUnixMS
	if delayMS < 0 {
		delayMS = 0
	}
	return float64(delayMS) / 1000
}

// streamProgressSuffixAt returns the localized factual char count plus snapshot
// freshness for the footer indicator, or "" when nothing may be shown. It reads
// cached state only — no I/O — and re-checks recipient identity so a reading can
// never render beside a different recipient.
func (m MailModel) streamProgressSuffixAt(state string, now time.Time) string {
	if !strings.EqualFold(state, "ACTIVE") || !m.streamProgress.ok || !m.streamProgress.snapshot.Active {
		return ""
	}
	if m.streamProgress.recipient != m.visibleStreamRecipient() {
		return ""
	}
	snapshot := m.streamProgress.snapshot
	return " · " + i18n.TF("mail.stream_progress_chars", formatStreamedChars(snapshot.StreamedChars)) +
		" · " + i18n.TF("mail.stream_progress_delay", streamProgressDelaySeconds(snapshot.UpdatedUnixMS, now))
}

func (m MailModel) streamProgressSuffix(state string) string {
	return m.streamProgressSuffixAt(state, time.Now())
}
