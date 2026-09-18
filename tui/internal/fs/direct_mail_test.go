package fs

import (
	"path/filepath"
	"reflect"
	"testing"
)

type directTargetTestInput = DirectTarget

func isDirectMailForTest(msg MailMessage, humanAddress string, target directTargetTestInput) bool {
	return IsDirectMail(msg, humanAddress, target)
}

func directThreadKeyForTest(target directTargetTestInput) string {
	return DirectThreadKey(target)
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
		{name: "matching padded identities remain literal", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": " id-agent-b "}}, target: directTargetTestInput{ProjectDirectory: "/project", Directory: "/project/.lingtai/agent-b", AgentID: " id-agent-b ", Address: agentB}, want: true},
		{name: "legacy target to human without identity", msg: MailMessage{From: agentB, To: human}, target: agentBTarget, want: true},
		{name: "legacy target without manifest id still uses policy a address fallback", msg: MailMessage{From: agentB, To: human}, target: directTargetTestInput{ProjectDirectory: "/project", Directory: "/project/.lingtai/agent-b", Address: agentB}, want: true},
		{name: "supplied mismatching identity is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "id-main"}}, target: agentBTarget, want: false},
		{name: "supplied padded identity is a literal mismatch", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": " id-agent-b "}}, target: agentBTarget, want: false},
		{name: "supplied nil identity is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": nil}}, target: agentBTarget, want: false},
		{name: "supplied identity with missing target id is not direct", msg: MailMessage{From: agentB, To: human, Identity: map[string]interface{}{"agent_id": "id-agent-b"}}, target: directTargetTestInput{ProjectDirectory: "/project", Directory: "/project/.lingtai/agent-b", Address: agentB}, want: false},
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

// The TUI and project manifests name agents by bare address, while a real
// email.reply envelope records the canonical absolute agent directories. Both
// spellings of the human and the selected target belong to the same thread, and
// nothing else does.
func TestIsDirectMailCanonicalProjectRoutes(t *testing.T) {
	const human = "human"
	project := t.TempDir()
	network := filepath.Join(project, ".lingtai")
	humanDir := filepath.Join(network, "human")
	lunaDir := filepath.Join(network, "luna")
	otherDir := filepath.Join(network, "other")
	foreignNetwork := filepath.Join(t.TempDir(), ".lingtai")
	foreignHumanDir := filepath.Join(foreignNetwork, "human")
	foreignLunaDir := filepath.Join(foreignNetwork, "luna")
	sep := string(filepath.Separator)

	luna := directTargetTestInput{ProjectDirectory: project, Directory: lunaDir, AgentID: "id-luna", Address: "luna"}
	lunaIdentity := map[string]interface{}{"agent_id": "id-luna"}
	humanTarget := directTargetTestInput{ProjectDirectory: project, Directory: humanDir, AgentID: "id-human", Address: humanDir}

	tests := []struct {
		name   string
		msg    MailMessage
		target directTargetTestInput
		want   bool
	}{
		{name: "bare human to bare target", msg: MailMessage{From: human, To: []string{"luna"}}, target: luna, want: true},
		{name: "absolute target reply to absolute human", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}, Identity: lunaIdentity}, target: luna, want: true},
		{name: "absolute target reply with scalar recipient", msg: MailMessage{From: lunaDir, To: humanDir, Identity: lunaIdentity}, target: luna, want: true},
		{name: "absolute target reply without identity", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}}, target: luna, want: true},
		{name: "bare target to absolute human", msg: MailMessage{From: "luna", To: humanDir, Identity: lunaIdentity}, target: luna, want: true},
		{name: "absolute target to bare human", msg: MailMessage{From: lunaDir, To: human, Identity: lunaIdentity}, target: luna, want: true},
		{name: "absolute human to bare target", msg: MailMessage{From: humanDir, To: []string{"luna"}}, target: luna, want: true},
		{name: "bare human to absolute target", msg: MailMessage{From: human, To: []string{lunaDir}}, target: luna, want: true},
		{name: "absolute human to absolute target", msg: MailMessage{From: humanDir, To: []string{lunaDir}}, target: luna, want: true},
		{name: "absolute forms compare lexically cleaned", msg: MailMessage{From: lunaDir + sep, To: []string{network + sep + "." + sep + "human"}, Identity: lunaIdentity}, target: luna, want: true},

		{name: "absolute reply with contradicting identity", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}, Identity: map[string]interface{}{"agent_id": "id-other"}}, target: luna, want: false},
		{name: "absolute reply with cc", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}, CC: []string{otherDir}, Identity: lunaIdentity}, target: luna, want: false},
		{name: "absolute reply with two recipients", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir, otherDir}, Identity: lunaIdentity}, target: luna, want: false},
		{name: "absolute reply with duplicate recipients", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir, humanDir}, Identity: lunaIdentity}, target: luna, want: false},
		{name: "same-project agent that is not the target", msg: MailMessage{From: otherDir, To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "human to same-project agent that is not the target", msg: MailMessage{From: humanDir, To: []string{otherDir}}, target: luna, want: false},
		{name: "cross-project agent with the same final component", msg: MailMessage{From: foreignLunaDir, To: []interface{}{humanDir}, Identity: lunaIdentity}, target: luna, want: false},
		{name: "cross-project human with the same final component", msg: MailMessage{From: lunaDir, To: []interface{}{foreignHumanDir}, Identity: lunaIdentity}, target: luna, want: false},
		{name: "cross-project agent to cross-project human", msg: MailMessage{From: foreignLunaDir, To: []interface{}{foreignHumanDir}}, target: luna, want: false},
		{name: "human to cross-project agent with the same final component", msg: MailMessage{From: humanDir, To: []string{foreignLunaDir}}, target: luna, want: false},
		{name: "arbitrary absolute sender", msg: MailMessage{From: filepath.Join(t.TempDir(), "luna"), To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "root absolute sender", msg: MailMessage{From: sep + "luna", To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "path below the target directory", msg: MailMessage{From: filepath.Join(lunaDir, "mailbox"), To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "network directory itself", msg: MailMessage{From: network, To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "relative path with the same final component", msg: MailMessage{From: filepath.Join(".lingtai", "luna"), To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "parent-relative path with the same final component", msg: MailMessage{From: ".." + sep + "luna", To: []interface{}{humanDir}}, target: luna, want: false},
		{name: "bare path with the same final component", msg: MailMessage{From: "elsewhere/luna", To: human}, target: luna, want: false},
		{name: "target without a directory has no absolute spelling", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}}, target: directTargetTestInput{ProjectDirectory: project, AgentID: "id-luna", Address: "luna"}, want: false},
		{name: "target without a project cannot resolve the human directory", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}}, target: directTargetTestInput{Directory: lunaDir, AgentID: "id-luna", Address: "luna"}, want: false},
		{name: "relative project cannot resolve the human directory", msg: MailMessage{From: lunaDir, To: []interface{}{humanDir}}, target: directTargetTestInput{ProjectDirectory: "project", Directory: lunaDir, AgentID: "id-luna", Address: "luna"}, want: false},
		{name: "human absolute directory is not its own target", msg: MailMessage{From: human, To: []string{humanDir}}, target: humanTarget, want: false},
		{name: "human bare address is not the target of an absolute human", msg: MailMessage{From: humanDir, To: []string{human}}, target: directTargetTestInput{ProjectDirectory: project, Directory: humanDir, AgentID: "id-human", Address: human}, want: false},
		{name: "human directory is not the directory of an aliased target", msg: MailMessage{From: human, To: []string{humanDir}}, target: directTargetTestInput{ProjectDirectory: project, Directory: humanDir, AgentID: "id-human", Address: "alias"}, want: false},
		{name: "aliased target in the human directory cannot send to the human", msg: MailMessage{From: "alias", To: []string{humanDir}}, target: directTargetTestInput{ProjectDirectory: project, Directory: humanDir, AgentID: "id-human", Address: "alias"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDirectMailForTest(tt.msg, human, tt.target); got != tt.want {
				t.Fatalf("isDirectMailForTest(%#v, %q, %#v) = %v, want %v", tt.msg, human, tt.target, got, tt.want)
			}
		})
	}
}

// The accepted-mail publication must bucket a real inbound reply into the
// selected agent's thread under the same equivalence IsDirectMail applies.
func TestDirectMailPublicationBucketsCanonicalProjectRoutes(t *testing.T) {
	const human = "human"
	project := t.TempDir()
	network := filepath.Join(project, ".lingtai")
	humanDir := filepath.Join(network, "human")
	foreignNetwork := filepath.Join(t.TempDir(), ".lingtai")
	foreignHumanDir := filepath.Join(foreignNetwork, "human")

	target := func(name string) DirectTarget {
		return DirectTarget{ProjectDirectory: project, Directory: filepath.Join(network, name), AgentID: "id-" + name, Address: name}
	}
	luna, other := target("luna"), target("other")
	// A legacy manifest may already carry the absolute directory as its address.
	legacy := target("legacy")
	legacy.Address = legacy.Directory
	identity := func(agentID string) map[string]interface{} { return map[string]interface{}{"agent_id": agentID} }

	accepted := []MailMessage{
		{MailboxID: "sent-bare", From: human, To: []string{"luna"}},
		{MailboxID: "reply-absolute", From: luna.Directory, To: []interface{}{humanDir}, Identity: identity("id-luna")},
		{MailboxID: "reply-other", From: other.Directory, To: []interface{}{humanDir}, Identity: identity("id-other")},
		{MailboxID: "sent-absolute", From: humanDir, To: []string{luna.Directory}},
		{MailboxID: "reply-foreign-project", From: filepath.Join(foreignNetwork, "luna"), To: []interface{}{humanDir}, Identity: identity("id-luna")},
		{MailboxID: "reply-foreign-human", From: luna.Directory, To: []interface{}{foreignHumanDir}, Identity: identity("id-luna")},
		{MailboxID: "reply-arbitrary-path", From: filepath.Join(t.TempDir(), "luna"), To: []interface{}{humanDir}},
		{MailboxID: "reply-wrong-identity", From: luna.Directory, To: []interface{}{humanDir}, Identity: identity("id-other")},
		{MailboxID: "reply-cc", From: luna.Directory, To: []interface{}{humanDir}, CC: []string{other.Directory}, Identity: identity("id-luna")},
		{MailboxID: "legacy-reply", From: legacy.Directory, To: []interface{}{humanDir}, Identity: identity("id-legacy")},
		{MailboxID: "legacy-sent", From: human, To: []string{legacy.Directory}},
	}
	publication := NewDirectMailPublication(human, []DirectTarget{luna, other, legacy}, accepted)

	for _, tt := range []struct {
		name   string
		target DirectTarget
		want   []string
	}{
		{name: "luna", target: luna, want: []string{"sent-bare", "reply-absolute", "sent-absolute"}},
		{name: "other", target: other, want: []string{"reply-other"}},
		{name: "legacy absolute address", target: legacy, want: []string{"legacy-reply", "legacy-sent"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, hasOlder := publication.DirectPage(tt.target, 10)
			if hasOlder {
				t.Fatal("DirectPage reported older mail for a short thread")
			}
			got := make([]string, len(page))
			for index, message := range page {
				got[index] = message.MailboxID
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("DirectPage(%s) mailbox IDs = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// Two projects may each hold an agent with the same bare address; an absolute
// envelope names only its own project's agent.
func TestDirectMailPublicationDoesNotBucketAcrossProjectsBySharedFinalComponent(t *testing.T) {
	const human = "human"
	projectA, projectB := t.TempDir(), t.TempDir()
	targetIn := func(project string) DirectTarget {
		return DirectTarget{ProjectDirectory: project, Directory: filepath.Join(project, ".lingtai", "luna"), AgentID: "id-" + filepath.Base(project), Address: "luna"}
	}
	lunaA, lunaB := targetIn(projectA), targetIn(projectB)
	replyFor := func(id string, target DirectTarget) MailMessage {
		return MailMessage{
			MailboxID: id,
			From:      target.Directory,
			To:        []interface{}{filepath.Join(target.ProjectDirectory, ".lingtai", "human")},
			Identity:  map[string]interface{}{"agent_id": target.AgentID},
		}
	}
	accepted := []MailMessage{replyFor("reply-a", lunaA), replyFor("reply-b", lunaB)}
	publication := NewDirectMailPublication(human, []DirectTarget{lunaA, lunaB}, accepted)

	for _, tt := range []struct {
		name   string
		target DirectTarget
		want   string
	}{
		{name: "project A", target: lunaA, want: "reply-a"},
		{name: "project B", target: lunaB, want: "reply-b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, _ := publication.DirectPage(tt.target, 10)
			if len(page) != 1 || page[0].MailboxID != tt.want {
				t.Fatalf("DirectPage(%s) = %#v, want only %s", tt.name, page, tt.want)
			}
		})
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
	paddedAgentID := directTargetTestInput{ProjectDirectory: "/project-a", Directory: "/project-a/.lingtai/padded", AgentID: " id-agent ", Address: "padded"}

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
	if got := directThreadKeyForTest(paddedAgentID); got == "" || got == beforeKey {
		t.Errorf("literal padded agent_id key = %q, want nonempty and distinct from %q", got, beforeKey)
	}
	if got := directThreadKeyForTest(directTargetTestInput{ProjectDirectory: "/project-a", AgentID: "   ", Address: "new"}); got != "" {
		t.Errorf("whitespace-only agent_id produced thread key %q", got)
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
