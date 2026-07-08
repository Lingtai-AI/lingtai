package fs

import "testing"

func TestResolveMailToUsesRealRecipients(t *testing.T) {
	humanAddr := "/project/.lingtai/human"
	tests := []struct {
		name string
		to   interface{}
		want string
	}{
		{
			name: "manager reply to human",
			to:   "human",
			want: "human",
		},
		{
			name: "human direct to repairman string",
			to:   "repairman",
			want: "repairman",
		},
		{
			name: "human direct to repairman list",
			to:   []interface{}{"repairman"},
			want: "repairman",
		},
		{
			name: "repairman reply to human list",
			to:   []interface{}{"human"},
			want: "human",
		},
		{
			name: "absolute agent path uses basename",
			to:   "/project/.lingtai/repairman",
			want: "repairman",
		},
		{
			name: "absolute human path maps to human",
			to:   humanAddr,
			want: "human",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMailTo(MailMessage{To: tt.to}, humanAddr, "manager")
			if got != tt.want {
				t.Fatalf("resolveMailTo(%#v) = %q, want %q", tt.to, got, tt.want)
			}
		})
	}
}
