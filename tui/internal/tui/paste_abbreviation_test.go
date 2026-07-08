package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAbbrevPasteContent(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		abbrev bool
	}{
		{
			name:   "empty",
			input:  "",
			want:   "",
			abbrev: false,
		},
		{
			name:   "single image",
			input:  "/Users/brian/Desktop/screenshot.png",
			want:   "[image1]",
			abbrev: true,
		},
		{
			name:   "two images",
			input:  "/path/to/img1.png\n/path/to/img2.jpg",
			want:   "[image1][image2]",
			abbrev: true,
		},
		{
			name:   "three images with trailing newline",
			input:  "/a.png\n/b.jpg\n/c.gif\n",
			want:   "[image1][image2][image3]",
			abbrev: true,
		},
		{
			name:   "short text - no abbreviation",
			input:  "Hello, world!",
			want:   "",
			abbrev: false,
		},
		{
			name:   "two lines - no abbreviation",
			input:  "line 1\nline 2",
			want:   "",
			abbrev: false,
		},
		{
			name:   "three lines - no abbreviation",
			input:  "a\nb\nc",
			want:   "",
			abbrev: false,
		},
		{
			name:   "four lines - abbreviate",
			input:  "a\nb\nc\nd",
			want:   "[pasted ~4 lines]",
			abbrev: true,
		},
		{
			name:   "twelve lines - abbreviate",
			input:  strings.Repeat("line\n", 12),
			want:   "[pasted ~12 lines]",
			abbrev: true,
		},
		{
			name:   "mixed image and text - no abbreviation",
			input:  "Check this: /path/to/img.png\nAnother line",
			want:   "",
			abbrev: false,
		},
		{
			name:   "image with spaces in path",
			input:  "/path/to/my image.png",
			want:   "[image1]",
			abbrev: true,
		},
		{
			name:   "various image extensions",
			input:  "/a.png\n/b.jpeg\n/c.webp\n/d.heic",
			want:   "[image1][image2][image3][image4]",
			abbrev: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := abbrevPasteContent(tt.input)
			if ok != tt.abbrev {
				t.Errorf("abbrevPasteContent() abbrev = %v, want %v", ok, tt.abbrev)
			}
			if got != tt.want {
				t.Errorf("abbrevPasteContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPasteImagesAbbreviatedInInput(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := "/Users/brian/Desktop/img1.png\n/Users/brian/Desktop/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	// Input should show abbreviated form
	if got := m.input.Value(); got != "[image1][image2]" {
		t.Errorf("input value = %q, want %q", got, "[image1][image2]")
	}
	// Actual paste content should be stored
	if m.pasteContent != content {
		t.Errorf("pasteContent = %q, want %q", m.pasteContent, content)
	}
	if m.pasteDisplay != "[image1][image2]" {
		t.Errorf("pasteDisplay = %q, want %q", m.pasteDisplay, "[image1][image2]")
	}
}

func TestPasteLongTextAbbreviatedInInput(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := strings.Repeat("some text line\n", 12)
	m, _ = m.Update(tea.PasteMsg{Content: content})

	if got := m.input.Value(); got != "[pasted ~12 lines]" {
		t.Errorf("input value = %q, want %q", got, "[pasted ~12 lines]")
	}
	if m.pasteContent != content {
		t.Errorf("pasteContent length = %d, want %d", len(m.pasteContent), len(content))
	}
}

func TestPasteShortTextNoAbbreviation(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := "Hello, world!"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	if got := m.input.Value(); got != content {
		t.Errorf("input value = %q, want %q", got, content)
	}
	if m.pasteContent != "" {
		t.Errorf("pasteContent should be empty for short text, got %q", m.pasteContent)
	}
}

func TestPasteContentClearedAfterSend(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := "/path/to/img1.png\n/path/to/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	if m.pasteContent == "" {
		t.Fatal("pasteContent should be set after paste")
	}

	// Simulate send (orchestrator is empty so WriteMail is not called)
	m, _ = m.Update(SendMsg{})

	// After send, paste state should be cleared
	if m.pasteContent != "" {
		t.Errorf("pasteContent should be cleared after send, got %q", m.pasteContent)
	}
	if m.pasteDisplay != "" {
		t.Errorf("pasteDisplay should be cleared after send, got %q", m.pasteDisplay)
	}
}

func TestPasteAbbreviationExpandedOnSend(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := "/path/to/img1.png\n/path/to/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	// After paste, input shows abbreviated form
	if m.input.Value() != "[image1][image2]" {
		t.Fatalf("input value = %q, want %q", m.input.Value(), "[image1][image2]")
	}

	// Simulate the expansion logic from SendMsg handler
	text := m.input.Value()
	if m.pasteContent != "" && m.pasteDisplay != "" {
		if idx := strings.Index(text, m.pasteDisplay); idx >= 0 {
			text = text[:idx] + m.pasteContent + text[idx+len(m.pasteDisplay):]
		}
	}
	if text != content {
		t.Errorf("expanded text = %q, want %q", text, content)
	}
}

func TestPasteAbbreviationWithSurroundingText(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// User types something, then pastes
	m.input.SetValue("Check this: ")
	m, _ = m.Update(tea.PasteMsg{Content: "/path/to/img.png"})

	// Input should have the typed text + abbreviation
	got := m.input.Value()
	if got != "Check this: [image1]" {
		t.Errorf("input value = %q, want %q", got, "Check this: [image1]")
	}

	// Simulate expansion
	text := got
	if m.pasteContent != "" && m.pasteDisplay != "" {
		if idx := strings.Index(text, m.pasteDisplay); idx >= 0 {
			text = text[:idx] + m.pasteContent + text[idx+len(m.pasteDisplay):]
		}
	}
	want := "Check this: /path/to/img.png"
	if text != want {
		t.Errorf("expanded text = %q, want %q", text, want)
	}
}

// ctrlEPress constructs a KeyPressMsg for ctrl+e.
func ctrlEPress() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
}

func TestPasteExpandCollapseToggle(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := "/path/to/img1.png\n/path/to/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	// After paste, input shows abbreviated form
	if m.input.Value() != "[image1][image2]" {
		t.Fatalf("after paste: input = %q, want %q", m.input.Value(), "[image1][image2]")
	}
	if m.pasteExpanded {
		t.Fatal("pasteExpanded should be false after paste")
	}

	// Press ctrl+e to expand
	m, _ = m.Update(ctrlEPress())
	if !m.pasteExpanded {
		t.Fatal("pasteExpanded should be true after ctrl+e")
	}
	if m.input.Value() != content {
		t.Errorf("after expand: input = %q, want %q", m.input.Value(), content)
	}

	// Press ctrl+e again to collapse
	m, _ = m.Update(ctrlEPress())
	if m.pasteExpanded {
		t.Fatal("pasteExpanded should be false after second ctrl+e")
	}
	if m.input.Value() != "[image1][image2]" {
		t.Errorf("after collapse: input = %q, want %q", m.input.Value(), "[image1][image2]")
	}
}

func TestPasteExpandCollapseWithSurroundingText(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.input.SetValue("See: ")
	content := "/path/to/img1.png\n/path/to/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content})

	wantCollapsed := "See: [image1][image2]"
	if m.input.Value() != wantCollapsed {
		t.Fatalf("after paste: input = %q, want %q", m.input.Value(), wantCollapsed)
	}

	// Expand
	m, _ = m.Update(ctrlEPress())
	wantExpanded := "See: " + content
	if m.input.Value() != wantExpanded {
		t.Errorf("after expand: input = %q, want %q", m.input.Value(), wantExpanded)
	}

	// Collapse
	m, _ = m.Update(ctrlEPress())
	if m.input.Value() != wantCollapsed {
		t.Errorf("after collapse: input = %q, want %q", m.input.Value(), wantCollapsed)
	}
}

func TestCtrlEPassthroughWhenNoAbbreviation(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Type some text (no paste abbreviation active)
	m.input.SetValue("hello")
	m, _ = m.Update(ctrlEPress())

	// ctrl+e should pass through to textarea when no abbreviation is active.
	// The textarea treats ctrl+e as "end of line" (no-op for single-line input),
	// so the value should remain unchanged.
	if m.input.Value() != "hello" {
		t.Errorf("ctrl+e with no abbreviation: input = %q, want %q", m.input.Value(), "hello")
	}
}

func TestPasteExpandedResetOnNewPaste(t *testing.T) {
	m := NewMailModel("", "", "", "", "codex", 10, "", "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content1 := "/path/to/img1.png\n/path/to/img2.png"
	m, _ = m.Update(tea.PasteMsg{Content: content1})

	// Expand the first paste
	m, _ = m.Update(ctrlEPress())
	if !m.pasteExpanded {
		t.Fatal("pasteExpanded should be true after ctrl+e")
	}

	// Paste something new — old expanded content collapses first,
	// then the new paste is abbreviated and appended.
	content2 := "/path/to/other.png"
	m, _ = m.Update(tea.PasteMsg{Content: content2})
	if m.pasteExpanded {
		t.Error("pasteExpanded should be reset to false on new paste")
	}
	want := "[image1][image2][image1]"
	if m.input.Value() != want {
		t.Errorf("after new paste: input = %q, want %q", m.input.Value(), want)
	}
}