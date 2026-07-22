package fs

import (
	"reflect"
	"testing"
)

type directTargetTestInput struct {
	ProjectDirectory string
	Directory        string
	AgentID          string
	Address          string
}

// These adapters keep the RED contract executable against the prior public API.
// The implementation change will replace the address-only calls with the new
// project-Agent DirectTarget boundary without changing the table expectations.
func isDirectMailForTest(msg MailMessage, humanAddress string, target directTargetTestInput) bool {
	return IsDirectMail(msg, humanAddress, target.Address)
}

func directThreadKeyForTest(target directTargetTestInput) string {
	return AddressFingerprint(target.Address)
}

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

	mainTarget := directTargetTestInput{
		ProjectDirectory: "/project",
		Directory:        "/project/.lingtai/main",
		AgentID:          "id-main",
		Address:          main,
	}
	agentBTarget := directTargetTestInput{
		ProjectDirectory: "/project",
		Directory:        "/project/.lingtai/agent-b",
		AgentID:          "id-agent-b",
		Address:          agentB,
	}

	tests := []struct {
		name   string
		msg    MailMessage
		target directTargetTestInput
		want   bool
	}{
		{name: "human to scalar target", msg: MailMessage{From: human, To: main}, target: mainTarget, want: true},
		{name: "human to singleton list target", msg: MailMessage{From: human, To: []interface{}{main}, Identity: map[string]interface{}{"agent_id": "id-human"}}, target: mainTarget, want: true},
		{name: "target to scalar human with matching identity", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "id-agent-b"}}, target: agentBTarget, want: true},
		{name: "legacy target to human without identity", msg: MailMessage{From: agentB, To: human}, target: agentBTarget, want: true},
		{name: "supplied mismatching identity is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "id-main"}}, target: agentBTarget, want: false},
		{name: "supplied empty identity is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "  "}}, target: agentBTarget, want: false},
		{name: "supplied non-string identity is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": 7}}, target: agentBTarget, want: false},
		{name: "human multi-to is not direct for main", msg: MailMessage{From: human, To: []interface{}{main, agentB}}, target: mainTarget, want: false},
		{name: "human multi-to is not direct for b", msg: MailMessage{From: human, To: []string{main, agentB}}, target: agentBTarget, want: false},
		{name: "target multi-to is not direct", msg: MailMessage{From: agentB, To: []interface{}{human, main}}, target: agentBTarget, want: false},
		{name: "heterogeneous target list is not direct", msg: MailMessage{From: human, To: []interface{}{main, 7}}, target: mainTarget, want: false},
		{name: "nil-bearing target list is not direct", msg: MailMessage{From: human, To: []interface{}{main, nil}}, target: mainTarget, want: false},
		{name: "extra empty target is not direct", msg: MailMessage{From: human, To: []interface{}{main, ""}}, target: mainTarget, want: false},
		{name: "extra whitespace target is not direct", msg: MailMessage{From: human, To: []string{main, "  "}}, target: mainTarget, want: false},
		{name: "duplicate raw targets are not a singleton envelope", msg: MailMessage{From: human, To: []interface{}{main, main}}, target: mainTarget, want: false},
		{name: "third party sender is not direct", msg: MailMessage{From: agentB, To: main}, target: mainTarget, want: false},
		{name: "cc cannot create target membership", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: mainTarget, want: false},
		{name: "cc prevents otherwise exact incoming mail", msg: MailMessage{From: agentB, To: human, CC: []string{main}}, target: agentBTarget, want: false},
		{name: "cc prevents otherwise exact outgoing mail", msg: MailMessage{From: human, To: main, CC: []string{agentB}}, target: mainTarget, want: false},
		{name: "human cc only is not direct", msg: MailMessage{From: human, To: agentB, CC: []string{main}}, target: mainTarget, want: false},
		{name: "surrounding whitespace is not identity", msg: MailMessage{From: " " + human + " ", To: []string{" " + main + " "}}, target: directTargetTestInput{ProjectDirectory: "/project", AgentID: "id-main", Address: " " + main + " "}, want: true},
		{name: "different agent is excluded from main projection", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "id-agent-b"}}, target: mainTarget, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDirectMailForTest(tt.msg, human, tt.target); got != tt.want {
				t.Fatalf("isDirectMailForTest(%#v, %q, %#v) = %v, want %v", tt.msg, human, tt.target, got, tt.want)
			}
		})
	}

	if isDirectMailForTest(MailMessage{From: human, To: main}, "", mainTarget) {
		t.Fatal("empty human address created direct-thread membership")
	}
	if isDirectMailForTest(MailMessage{From: human, To: main}, human, directTargetTestInput{ProjectDirectory: "/project", AgentID: "id-main"}) {
		t.Fatal("empty target address created direct-thread membership")
	}
	if isDirectMailForTest(MailMessage{From: human, To: human}, human, directTargetTestInput{ProjectDirectory: "/project", Directory: "/project/.lingtai/human", AgentID: "id-human", Address: human}) {
		t.Fatal("human address was accepted as its own Agent target")
	}
}

func TestDirectTargetCarriesStableIdentityAndRoutingData(t *testing.T) {
	targetType := reflect.TypeOf(DirectTarget{})
	for _, field := range []string{"ProjectDirectory", "Directory", "AgentID", "Address"} {
		if _, ok := targetType.FieldByName(field); !ok {
			t.Errorf("DirectTarget missing %s", field)
		}
	}
}

func TestDirectThreadKeyUsesProjectAndAgentID(t *testing.T) {
	beforeRename := directTargetTestInput{ProjectDirectory: "/project-a", Directory: "/project-a/.lingtai/old", AgentID: "id-agent", Address: "old"}
	afterRename := directTargetTestInput{ProjectDirectory: "/project-a", Directory: "/project-a/.lingtai/new", AgentID: "id-agent", Address: "new"}
	otherAgent := directTargetTestInput{ProjectDirectory: "/project-a", Directory: "/project-a/.lingtai/new", AgentID: "id-other", Address: "new"}
	otherProject := directTargetTestInput{ProjectDirectory: "/project-b", Directory: "/project-b/.lingtai/new", AgentID: "id-agent", Address: "new"}

	beforeKey := directThreadKeyForTest(beforeRename)
	if beforeKey == "" {
		t.Fatal("complete project-Agent target produced an empty thread key")
	}
	if got := directThreadKeyForTest(afterRename); got != beforeKey {
		t.Errorf("address/directory rename changed thread key: before %q after %q", beforeKey, got)
	}
	if got := directThreadKeyForTest(otherAgent); got == beforeKey {
		t.Errorf("different agent_id shared thread key %q", got)
	}
	if got := directThreadKeyForTest(otherProject); got == beforeKey {
		t.Errorf("same agent_id in another project shared thread key %q", got)
	}
	if got := directThreadKeyForTest(directTargetTestInput{AgentID: "id-agent", Address: "new"}); got != "" {
		t.Errorf("missing project directory produced thread key %q", got)
	}
	if got := directThreadKeyForTest(directTargetTestInput{ProjectDirectory: "/project-a", Address: "new"}); got != "" {
		t.Errorf("missing agent_id produced thread key %q", got)
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
