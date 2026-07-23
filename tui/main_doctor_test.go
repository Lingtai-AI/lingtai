package main

import "testing"

func TestDispatchDoctor(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantHelp   bool
		wantDoctor bool
	}{
		{name: "no arguments runs doctor", wantDoctor: true},
		{name: "help flag shows help", args: []string{"--help"}, wantHelp: true},
		{name: "short help flag shows help", args: []string{"-h"}, wantHelp: true},
		{name: "ordinary argument preserves doctor path", args: []string{"--force"}, wantDoctor: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var helpCalls, doctorCalls int

			dispatchDoctor(tt.args,
				func() { helpCalls++ },
				func() { doctorCalls++ },
			)

			if got := helpCalls == 1; got != tt.wantHelp {
				t.Fatalf("help calls = %d, want help called = %t", helpCalls, tt.wantHelp)
			}
			if got := doctorCalls == 1; got != tt.wantDoctor {
				t.Fatalf("doctor calls = %d, want doctor called = %t", doctorCalls, tt.wantDoctor)
			}
		})
	}
}
