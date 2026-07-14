package fs

import (
	"reflect"
	"testing"
)

func TestNormalizeMailEndpoints(t *testing.T) {
	tests := []struct {
		name string
		to   interface{}
		want []string
	}{
		{name: "string", to: "agent-a", want: []string{"agent-a"}},
		{name: "typed list", to: []string{"agent-a", "agent-b"}, want: []string{"agent-a", "agent-b"}},
		{name: "decoded list", to: []interface{}{"agent-a", 7, "agent-b"}, want: []string{"agent-a", "agent-b"}},
		{name: "trim empty and duplicates", to: []interface{}{" agent-a ", "", "agent-a"}, want: []string{"agent-a"}},
		{name: "unsupported", to: map[string]string{"to": "agent-a"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeMailEndpoints(tt.to); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeMailEndpoints(%#v) = %#v, want %#v", tt.to, got, tt.want)
			}
		})
	}
}

func TestIsDirectMail(t *testing.T) {
	const human = "project/human"
	const main = "project/main"
	const agentB = "project/agent-b"
	tests := []struct {
		name   string
		msg    MailMessage
		target string
		want   bool
	}{
		{name: "human to scalar target", msg: MailMessage{From: human, To: main}, target: main, want: true},
		{name: "human multi-to belongs to main", msg: MailMessage{From: human, To: []interface{}{main, agentB}}, target: main, want: true},
		{name: "human multi-to belongs to b", msg: MailMessage{From: human, To: []string{main, agentB}}, target: agentB, want: true},
		{name: "b multi-to belongs to b", msg: MailMessage{From: agentB, To: []interface{}{human, main}}, target: agentB, want: true},
		{name: "b multi-to does not belong to main", msg: MailMessage{From: agentB, To: []interface{}{human, main}}, target: main, want: false},
		{name: "cc does not create main membership", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: main, want: false},
		{name: "cc leaves b membership direct", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: agentB, want: true},
		{name: "human cc only is not direct", msg: MailMessage{From: human, To: agentB, CC: []string{main}}, target: main, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDirectMail(tt.msg, human, tt.target); got != tt.want {
				t.Fatalf("IsDirectMail(%#v, %q, %q) = %v, want %v", tt.msg, human, tt.target, got, tt.want)
			}
		})
	}
}
