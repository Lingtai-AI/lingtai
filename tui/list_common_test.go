package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseListArgsModesAndDir(t *testing.T) {
	opts, err := parseListArgs([]string{"--detailed", "--admin", "./project"})
	if err != nil {
		t.Fatalf("parseListArgs returned error: %v", err)
	}
	if !opts.Detailed || !opts.Admin {
		t.Fatalf("expected detailed/admin true, got detailed=%v admin=%v", opts.Detailed, opts.Admin)
	}
	want, _ := filepath.Abs("./project")
	if opts.FilterDir != want {
		t.Fatalf("FilterDir=%q, want %q", opts.FilterDir, want)
	}
}

func TestParseListArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseListArgs([]string{"--verbose"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func adminRawFromJSON(raw string) interface{} {
	var manifest map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &manifest)
	return manifest["admin"]
}

func TestSummarizeAdmin(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"null", `{"admin": null}`, "admin=null"},
		{"empty", `{"admin": {}}`, "admin={}"},
		{"sorted", `{"admin": {"nirvana": false, "karma": true}}`, "karma=true,nirvana=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarizeAdmin(adminRawFromJSON(tc.raw)); got != tc.want {
				t.Fatalf("summarizeAdmin=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestRoleLabel(t *testing.T) {
	if got := roleLabel(listAgentInfo{IsOrchestrator: true}); got != "MAIN" {
		t.Fatalf("orchestrator role=%q", got)
	}
	if got := roleLabel(listAgentInfo{IsHuman: true}); got != "HUMAN" {
		t.Fatalf("human role=%q", got)
	}
	if got := roleLabel(listAgentInfo{}); got != "AGENT" {
		t.Fatalf("agent role=%q", got)
	}
}

func TestPrintListDetailedAndAdmin(t *testing.T) {
	procs := []listProc{
		{
			PID:     "123",
			Uptime:  "1m 0s",
			Agent:   "mimo-1",
			Project: "/tmp/project",
			Dir:     "/tmp/project/.lingtai/mimo-1",
			Info: listAgentInfo{
				Address:        "mimo-1",
				AgentName:      "mimo-1",
				Nickname:       "Mimo",
				State:          "IDLE",
				IsOrchestrator: true,
				AdminSummary:   "karma=true",
				IMHandles:      "telegram:@Lingtaidev1bot",
			},
		},
	}

	var detailed bytes.Buffer
	printList(&detailed, procs, nil, listOptions{Detailed: true}, true)
	detailedOut := detailed.String()
	for _, want := range []string{"ROLE", "MAIN", "IDLE", "mimo-1", "Mimo", "telegram:@Lingtaidev1bot"} {
		if !strings.Contains(detailedOut, want) {
			t.Fatalf("detailed output missing %q:\n%s", want, detailedOut)
		}
	}

	var admin bytes.Buffer
	printList(&admin, procs, nil, listOptions{Admin: true, Detailed: true}, true)
	adminOut := admin.String()
	for _, want := range []string{"ADMIN", "karma=true", "MAIN"} {
		if !strings.Contains(adminOut, want) {
			t.Fatalf("admin output missing %q:\n%s", want, adminOut)
		}
	}
}
