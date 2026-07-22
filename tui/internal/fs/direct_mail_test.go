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
		{name: "nil", to: nil, want: nil},
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
		{name: "human to singleton list target", msg: MailMessage{From: human, To: []interface{}{main}}, target: main, want: true},
		{name: "target to scalar human", msg: MailMessage{From: agentB, To: human}, target: agentB, want: true},
		{name: "human multi-to is not direct for main", msg: MailMessage{From: human, To: []interface{}{main, agentB}}, target: main, want: false},
		{name: "human multi-to is not direct for b", msg: MailMessage{From: human, To: []string{main, agentB}}, target: agentB, want: false},
		{name: "target multi-to is not direct", msg: MailMessage{From: agentB, To: []interface{}{human, main}}, target: agentB, want: false},
		{name: "third party sender is not direct", msg: MailMessage{From: agentB, To: main}, target: main, want: false},
		{name: "cc cannot create target membership", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: main, want: false},
		{name: "cc prevents otherwise exact incoming mail", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: agentB, want: false},
		{name: "cc prevents otherwise exact outgoing mail", msg: MailMessage{From: human, To: main, CC: []string{agentB}}, target: main, want: false},
		{name: "human cc only is not direct", msg: MailMessage{From: human, To: agentB, CC: []string{main}}, target: main, want: false},
		{name: "surrounding whitespace is not identity", msg: MailMessage{From: " " + human + " ", To: []string{" " + main + " "}}, target: " " + main + " ", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDirectMail(tt.msg, human, tt.target); got != tt.want {
				t.Fatalf("IsDirectMail(%#v, %q, %q) = %v, want %v", tt.msg, human, tt.target, got, tt.want)
			}
		})
	}

	if IsDirectMail(MailMessage{From: human, To: main}, "", main) {
		t.Fatal("empty human address created direct-thread membership")
	}
	if IsDirectMail(MailMessage{From: human, To: main}, human, "") {
		t.Fatal("empty target address created direct-thread membership")
	}
}

func TestAddressFingerprintNormalizesOnlySurroundingWhitespace(t *testing.T) {
	if AddressFingerprint(" project/agent-b ") != AddressFingerprint("project/agent-b") {
		t.Fatal("surrounding whitespace changed address fingerprint")
	}
	if AddressFingerprint("project/agent-b") == AddressFingerprint("project/agent-c") {
		t.Fatal("distinct target addresses share a fingerprint")
	}
}
