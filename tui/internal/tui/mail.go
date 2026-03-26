package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ChatMessage represents a single message in the chat stream.
type ChatMessage struct {
	From      string
	To        string
	Subject   string
	Body      string
	Timestamp string
	IsFromMe  bool   // human sent this
	Type      string // "mail", "thinking", "diary"
}

// ViewChangeMsg requests the app to switch views.
type ViewChangeMsg struct {
	View string
}

type mailRefreshMsg struct{ messages []ChatMessage }
type tickMsg time.Time

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Every(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// MailModel is the main chat view — a single chronological stream.
type MailModel struct {
	humanDir     string
	humanAddr    string
	orchestrator string // 本我 directory path (full path under .lingtai/)
	orchAddr     string // 本我 address (from .agent.json)
	orchName     string // 本我 agent name
	baseDir      string // .lingtai/ directory
	verbose      bool
	messages     []ChatMessage
	viewport     viewport.Model
	input        InputModel
	palette      PaletteModel
	width        int
	height       int
	ready        bool
}

func NewMailModel(humanDir, humanAddr, baseDir, orchDir, orchName string) MailModel {
	input := NewInputModel()
	palette := NewPaletteModel()
	// Resolve orchestrator address from .agent.json
	orchAddr := orchDir
	if orchDir != "" {
		if node, err := fs.ReadAgent(orchDir); err == nil && node.Address != "" {
			orchAddr = node.Address
		}
	}
	return MailModel{
		humanDir:     humanDir,
		humanAddr:    humanAddr,
		baseDir:      baseDir,
		orchestrator: orchDir,
		orchAddr:     orchAddr,
		orchName:     orchName,
		input:        input,
		palette:      palette,
	}
}

func (m MailModel) refreshMail() tea.Msg {
	var chatMsgs []ChatMessage

	// Read inbox (messages FROM 本我 to human)
	inbox, _ := fs.ReadInbox(m.humanDir)
	for _, msg := range inbox {
		parts := strings.Split(msg.From, "/")
		fromName := parts[len(parts)-1]
		chatMsgs = append(chatMsgs, ChatMessage{
			From:      fromName,
			To:        i18n.T("mail.you"),
			Subject:   msg.Subject,
			Body:      msg.Message,
			Timestamp: msg.ReceivedAt,
			IsFromMe:  false,
			Type:      "mail",
		})
	}

	// Read sent (messages FROM human to 本我)
	sent, _ := fs.ReadSent(m.humanDir)
	for _, msg := range sent {
		chatMsgs = append(chatMsgs, ChatMessage{
			From:      i18n.T("mail.you"),
			To:        m.orchName,
			Subject:   msg.Subject,
			Body:      msg.Message,
			Timestamp: msg.ReceivedAt,
			IsFromMe:  true,
			Type:      "mail",
		})
	}

	// If verbose, read events
	if m.verbose && m.orchestrator != "" {
		eventsPath := filepath.Join(m.orchestrator, "logs", "events.jsonl")
		events := ReadEvents(eventsPath)
		chatMsgs = append(chatMsgs, events...)
	}

	// Sort by timestamp
	sort.Slice(chatMsgs, func(i, j int) bool {
		return chatMsgs[i].Timestamp < chatMsgs[j].Timestamp
	})

	return mailRefreshMsg{messages: chatMsgs}
}

func (m MailModel) Init() tea.Cmd {
	return tea.Batch(
		m.refreshMail,
		tickEvery(time.Second),
		m.input.Focus(),
	)
}

func (m MailModel) Update(msg tea.Msg) (MailModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width)
		headerHeight := 2 // title bar
		inputHeight := 2  // input bar + border
		vpHeight := msg.Height - headerHeight - inputHeight
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.SetContent(m.renderMessages())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		return m, nil

	case mailRefreshMsg:
		m.messages = msg.messages
		if m.ready {
			atBottom := m.viewport.AtBottom()
			m.viewport.SetContent(m.renderMessages())
			if atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refreshMail, tickEvery(time.Second))

	case PaletteSelectMsg:
		m.input.Reset()
		// Forward to app
		return m, func() tea.Msg { return PaletteSelectMsg{Command: msg.Command} }

	case tea.KeyMsg:
		// If palette is active, route to palette
		if m.input.IsPaletteActive() {
			switch msg.String() {
			case "enter", "up", "down":
				var cmd tea.Cmd
				m.palette, cmd = m.palette.Update(msg)
				return m, cmd
			case "esc":
				m.input.Reset()
				return m, nil
			default:
				// Forward typing to input, then update palette filter
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				// Extract filter from input (text after "/")
				val := m.input.Value()
				if len(val) > 1 {
					m.palette.SetFilter(val[1:])
				} else {
					m.palette.SetFilter("")
				}
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+o":
			m.verbose = !m.verbose
			return m, m.refreshMail

		case "enter":
			text := m.input.Value()
			if text != "" && m.orchestrator != "" {
				// Send mail to orchestrator
				fs.WriteMail(m.orchestrator, m.humanDir, m.humanAddr, m.orchAddr, "", text)
				m.input.Reset()
				return m, m.refreshMail
			}
			return m, nil

		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

		// If input is focused, forward keys to input
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Check if slash was typed
		if m.input.IsPaletteActive() {
			val := m.input.Value()
			if len(val) > 1 {
				m.palette.SetFilter(val[1:])
			} else {
				m.palette.SetFilter("")
			}
		}
		return m, cmd
	}

	return m, tea.Batch(cmds...)
}

func (m MailModel) renderMessages() string {
	if len(m.messages) == 0 {
		return "\n" + StyleSubtle.Render("  "+i18n.T("mail.no_messages"))
	}

	var b strings.Builder
	for _, msg := range m.messages {
		switch msg.Type {
		case "thinking":
			if m.verbose {
				line := StyleSubtle.Render(fmt.Sprintf("  \u250a [thinking] %s", msg.Body))
				b.WriteString(line + "\n")
			}
		case "diary":
			if m.verbose {
				line := StyleSubtle.Render(fmt.Sprintf("  \u250a [diary] %s", msg.Body))
				b.WriteString(line + "\n")
			}
		default: // "mail"
			if m.verbose {
				// Show headers
				header := StyleSubtle.Render(fmt.Sprintf("  \u250a %s \u2192 %s", msg.From, msg.To))
				if msg.Subject != "" {
					header += StyleSubtle.Render(fmt.Sprintf(" | Subject: %s", msg.Subject))
				}
				header += StyleSubtle.Render(fmt.Sprintf(" | %s", msg.Timestamp))
				b.WriteString(header + "\n")
			}

			nameStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true)
			if msg.IsFromMe {
				nameStyle = lipgloss.NewStyle().Foreground(ColorMail).Bold(true)
			}
			name := nameStyle.Render(msg.From)
			b.WriteString(fmt.Sprintf("\n  %s: %s\n", name, msg.Body))
		}
	}
	return b.String()
}

func (m MailModel) View() string {
	if !m.ready {
		return "\n  Loading..."
	}

	var b strings.Builder

	// Title bar
	title := StyleTitle.Render("  " + i18n.T("app.title") + " \u2014 " + m.orchName)
	verboseHint := ""
	if m.verbose {
		verboseHint = lipgloss.NewStyle().Foreground(ColorActive).Render("[" + i18n.T("mail.verbose_on") + "]")
	} else {
		verboseHint = StyleSubtle.Render("ctrl+o")
	}
	titleLine := title
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(verboseHint) - 1
	if padding > 0 {
		titleLine += strings.Repeat(" ", padding) + verboseHint
	}
	b.WriteString(titleLine + "\n")
	b.WriteString(strings.Repeat("\u2500", m.width) + "\n")

	// Viewport (message stream)
	b.WriteString(m.viewport.View())

	// Separator
	b.WriteString("\n" + strings.Repeat("\u2500", m.width) + "\n")

	// Palette overlay (if active)
	if m.input.IsPaletteActive() {
		b.WriteString(m.palette.View() + "\n")
	}

	// Input bar
	b.WriteString(m.input.View())

	return b.String()
}
