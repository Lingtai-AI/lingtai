package tui

import (
	"image/color"
	"testing"
)

func TestColorToHex(t *testing.T) {
	tests := []struct {
		name string
		c    color.Color
		want string
	}{
		{"black", color.RGBA{R: 0, G: 0, B: 0, A: 255}, "#000000"},
		{"white", color.RGBA{R: 255, G: 255, B: 255, A: 255}, "#ffffff"},
		{"red", color.RGBA{R: 255, G: 0, B: 0, A: 255}, "#ff0000"},
		{"blue", color.RGBA{R: 0, G: 0, B: 255, A: 255}, "#0000ff"},
		{"gray", color.RGBA{R: 128, G: 128, B: 128, A: 255}, "#808080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorToHex(tt.c)
			if got != tt.want {
				t.Errorf("colorToHex(%v) = %s, want %s", tt.c, got, tt.want)
			}
		})
	}
}
