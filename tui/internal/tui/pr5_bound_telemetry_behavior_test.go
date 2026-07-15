package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type pr5TelemetryEnvelopeView struct {
	kind               uint64
	fields             uint64
	projectID          string
	storeID            uint64
	activation         uint64
	targetDirectory    string
	addressFingerprint string
	targetPolicy       uint64
	pid                int64
	generation         uint64
}

func pr5CurrentTelemetryBinding(app App) pr5TelemetryEnvelopeView {
	current := app.asyncCurrent()
	return pr5TelemetryEnvelopeView{
		fields:             uint64(asyncHasOwner | asyncHasTarget | asyncHasGeneration),
		projectID:          current.binding.owner.projectID,
		storeID:            current.binding.owner.storeID,
		activation:         current.binding.owner.activation,
		targetDirectory:    current.binding.target.directory,
		addressFingerprint: current.binding.target.addressFingerprint,
		targetPolicy:       uint64(current.binding.target.policy),
		pid:                int64(current.binding.target.pid),
		generation:         current.binding.generation,
	}
}

func pr5RequireTelemetryEnvelope(
	t *testing.T,
	label string,
	msg homeTelemetryMsg,
	want pr5TelemetryEnvelopeView,
) uint64 {
	t.Helper()
	value := reflect.ValueOf(msg)
	envelope := value.FieldByName("envelope")
	if !envelope.IsValid() {
		t.Fatalf("%s homeTelemetryMsg missing async envelope; target identity is not expressible", label)
	}
	if envelope.Type() != reflect.TypeOf(asyncEnvelope{}) {
		t.Fatalf("%s homeTelemetryMsg envelope type = %v, want asyncEnvelope", label, envelope.Type())
	}
	owner := envelope.FieldByName("owner")
	target := envelope.FieldByName("target")
	generation := envelope.FieldByName("generation")
	got := pr5TelemetryEnvelopeView{
		kind:               envelope.FieldByName("kind").Uint(),
		fields:             envelope.FieldByName("fields").Uint(),
		projectID:          owner.FieldByName("projectID").String(),
		storeID:            owner.FieldByName("storeID").Uint(),
		activation:         owner.FieldByName("activation").Uint(),
		targetDirectory:    target.FieldByName("directory").String(),
		addressFingerprint: target.FieldByName("addressFingerprint").String(),
		targetPolicy:       target.FieldByName("policy").Uint(),
		pid:                target.FieldByName("pid").Int(),
		generation:         generation.FieldByName("thread").Uint(),
	}
	kind := got.kind
	got.kind = 0
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s telemetry envelope = %+v, want %+v", label, got, want)
	}
	return kind
}

func pr5RunTelemetryCmd(t *testing.T, label string, cmd tea.Cmd) homeTelemetryMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("%s telemetry command is nil", label)
	}
	raw := runCmd(cmd)
	msg, ok := raw.(homeTelemetryMsg)
	if !ok {
		t.Fatalf("%s telemetry command produced %T, want homeTelemetryMsg", label, raw)
	}
	return msg
}

func TestPR5Stage2OrdinaryTelemetryStaysBoundAndSettlesExactFlight(t *testing.T) {
	app, scanner, locations := installationNewApp(t, 0)
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	targetA := filepath.Join(app.projectDir, "agent-a")
	targetB := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetA, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetB, "agent-b", "Agent B", "Agent B")
	writeStatusJSON(t, targetA, 11, 1100, 10000)
	writeStatusJSON(t, targetB, 22, 2200, 10000)
	sentinel := pr5WriteMainInsightSentinel(t, app.projectDir)

	pr5BindCoordinatorRailTarget(t, &app, targetA, "Agent A", 4101, 1)
	wantA1 := pr5CurrentTelemetryBinding(app)
	cmdA1 := app.mail.maybeScheduleHomeTelemetry(time.Unix(100, 0))

	pr5BindCoordinatorRailTarget(t, &app, targetB, "Agent B", 4201, 2)
	wantB1 := pr5CurrentTelemetryBinding(app)
	cmdB1 := app.mail.maybeScheduleHomeTelemetry(time.Unix(200, 0))

	// Execute both value-receiver reads only after the active target moved. They
	// must still read their captured A1/B1 directories rather than current state.
	msgA1 := pr5RunTelemetryCmd(t, "A1", cmdA1)
	msgB1 := pr5RunTelemetryCmd(t, "B1", cmdB1)

	writeStatusJSON(t, targetA, 33, 3300, 10000)
	pr5BindCoordinatorRailTarget(t, &app, targetA, "Agent A", 4101, 3)
	wantA2 := pr5CurrentTelemetryBinding(app)
	cmdA2 := app.mail.maybeScheduleHomeTelemetry(time.Unix(300, 0))
	msgA2 := pr5RunTelemetryCmd(t, "A2", cmdA2)
	if !app.mail.homeTelemetryInFlight {
		t.Fatal("A2 telemetry fixture did not retain an exact physical flight")
	}

	kindA1 := pr5RequireTelemetryEnvelope(t, "A1", msgA1, wantA1)
	kindB1 := pr5RequireTelemetryEnvelope(t, "B1", msgB1, wantB1)
	kindA2 := pr5RequireTelemetryEnvelope(t, "A2", msgA2, wantA2)
	if kindA1 == 0 || kindA1 != kindB1 || kindA1 != kindA2 {
		t.Fatalf("telemetry envelope kinds = A1 %d B1 %d A2 %d, want one non-zero logical kind", kindA1, kindB1, kindA2)
	}
	if msgA1.t.contextUsage != 0.11 || msgA1.t.contextUsed != 1100 ||
		msgB1.t.contextUsage != 0.22 || msgB1.t.contextUsed != 2200 ||
		msgA2.t.contextUsage != 0.33 || msgA2.t.contextUsed != 3300 {
		t.Fatalf(
			"captured telemetry reads = A1 %.2f/%d B1 %.2f/%d A2 %.2f/%d, want 0.11/1100 0.22/2200 0.33/3300",
			msgA1.t.contextUsage,
			msgA1.t.contextUsed,
			msgB1.t.contextUsage,
			msgB1.t.contextUsed,
			msgA2.t.contextUsage,
			msgA2.t.contextUsed,
		)
	}

	before := installationSnapshot(app, locations)
	beforeThread := app.currentThread
	beforeScans := scanner.scans.Load()
	for label, stale := range map[string]homeTelemetryMsg{"A1": msgA1, "B1": msgB1} {
		updated, effect := installationDeliverApp(t, app, stale)
		if effect != nil {
			t.Fatalf("stale %s telemetry returned effect %T", label, runCmd(effect))
		}
		installationAssertAppState(t, "stale "+label+" telemetry", updated, locations, before)
		if !updated.mail.homeTelemetryInFlight || updated.mail.homeTelemetryLoaded ||
			!reflect.DeepEqual(updated.mail.homeTelemetry, homeTelemetry{}) || updated.mail.homeTelemetryLastFetch != (time.Time{}) {
			t.Fatalf(
				"stale %s telemetry settled/published A2: inFlight=%v loaded=%v telemetry=%+v lastFetch=%v",
				label,
				updated.mail.homeTelemetryInFlight,
				updated.mail.homeTelemetryLoaded,
				updated.mail.homeTelemetry,
				updated.mail.homeTelemetryLastFetch,
			)
		}
		if updated.currentThread != beforeThread || scanner.scans.Load() != beforeScans {
			t.Fatalf("stale %s telemetry changed thread/scanner: threadChanged=%v scans=%d want=%d", label, updated.currentThread != beforeThread, scanner.scans.Load(), beforeScans)
		}
	}

	accepted, effect := installationDeliverApp(t, app, msgA2)
	if effect != nil {
		t.Fatalf("exact A2 telemetry returned effect %T", runCmd(effect))
	}
	installationAssertAppState(t, "exact A2 telemetry", accepted, locations, before)
	if accepted.mail.homeTelemetryInFlight || !accepted.mail.homeTelemetryLoaded ||
		accepted.mail.homeTelemetry.contextUsage != 0.33 || accepted.mail.homeTelemetry.contextUsed != 3300 ||
		accepted.mail.homeTelemetryLastFetch == (time.Time{}) {
		t.Fatalf("exact A2 telemetry did not settle/publish once: inFlight=%v loaded=%v telemetry=%+v lastFetch=%v", accepted.mail.homeTelemetryInFlight, accepted.mail.homeTelemetryLoaded, accepted.mail.homeTelemetry, accepted.mail.homeTelemetryLastFetch)
	}
	if accepted.currentThread != beforeThread || scanner.scans.Load() != beforeScans {
		t.Fatalf("exact A2 telemetry changed thread/scanner: threadChanged=%v scans=%d want=%d", accepted.currentThread != beforeThread, scanner.scans.Load(), beforeScans)
	}

	acceptedAt := accepted.mail.homeTelemetryLastFetch
	duplicate, effect := installationDeliverApp(t, accepted, msgA2)
	if effect != nil {
		t.Fatalf("duplicate A2 telemetry returned effect %T", runCmd(effect))
	}
	installationAssertAppState(t, "duplicate A2 telemetry", duplicate, locations, before)
	if duplicate.mail.homeTelemetryInFlight || !reflect.DeepEqual(duplicate.mail.homeTelemetry, accepted.mail.homeTelemetry) ||
		duplicate.mail.homeTelemetryLastFetch != acceptedAt || duplicate.currentThread != beforeThread || scanner.scans.Load() != beforeScans {
		t.Fatalf("duplicate A2 telemetry re-settled/published: inFlight=%v telemetry=%+v lastFetchChanged=%v threadChanged=%v scans=%d want=%d", duplicate.mail.homeTelemetryInFlight, duplicate.mail.homeTelemetry, duplicate.mail.homeTelemetryLastFetch != acceptedAt, duplicate.currentThread != beforeThread, scanner.scans.Load(), beforeScans)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("ordinary telemetry removed Main's shared insight sentinel: %v", err)
	}
}
