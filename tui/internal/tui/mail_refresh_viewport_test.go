package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMailRefreshMsgRerendersReadyViewport(t *testing.T) {
	m := newSizedMailModel(t)
	if !m.ready {
		t.Fatal("precondition: model should be ready")
	}
	m.viewport.SetContent("stale before refresh")

	updated, _ := m.Update(mailRefreshMsg{alive: true, state: "IDLE"})
	if got := updated.viewport.View(); strings.Contains(got, "stale before refresh") {
		t.Fatalf("mailRefreshMsg left stale viewport content: %q", got)
	}
}

func TestMailRenderShowsLightweightRouteLabels(t *testing.T) {
	m := newSizedMailModel(t)
	m.verbose = verboseOff

	out := ansi.Strip(m.renderMessages([]ChatMessage{
		{
			Type:      "mail",
			From:      "human",
			To:        "repairman",
			Subject:   "do-not-render-subject-in-normal-chat",
			Body:      "hello repairman",
			IsFromMe:  true,
			Delivered: true,
		},
		{
			Type:       "mail",
			From:       "repairman",
			To:         "human",
			Subject:    "do-not-render-reply-subject-in-normal-chat",
			Body:       "hello human",
			IsFromOrch: true,
		},
	}))

	if !strings.Contains(out, "human → repairman") {
		t.Fatalf("rendered mail missing outbound route label; output=%q", out)
	}
	if !strings.Contains(out, "repairman → human") {
		t.Fatalf("rendered mail missing incoming route label; output=%q", out)
	}
	if strings.Contains(out, "do-not-render") {
		t.Fatalf("normal chat render should not include subject/envelope metadata; output=%q", out)
	}
}
