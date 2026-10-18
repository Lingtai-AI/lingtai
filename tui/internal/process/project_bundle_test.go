package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

// One narrow phase fixture: unrelated methods deliberately remain nil, so an
// extra native operation cannot silently succeed. Real native I/O is in preset.
type bundlePhaseFixture struct {
	ProjectBundleNative
	calls   int
	failure error
}

func (n *bundlePhaseFixture) LoadMetadata(source string) (ProjectBundleMetadata, error) {
	n.calls++
	return ProjectBundleMetadata{Name: "fixture"}, n.failure
}
func bundleFixtureFrame(s *bundleExchangeState, sequence int, operation string, args interface{}) []byte {
	return bundleJSON(map[string]interface{}{"status": "bundle_io", "binding": s.binding, "sequence": sequence, "operation": operation, "arguments": args})
}

// Exercise replay rejection without coupling the test to retained-error storage
// layout; the live-child test below covers transcript/error exposure.
func TestBundleRejectsReplayedNativePhase(t *testing.T) {
	native := &bundlePhaseFixture{failure: os.ErrPermission}
	s := newBundleExchange(ProjectBundleRequest{Kind: "disk", Source: "s", Destination: "d"}, native)
	frame := bundleFixtureFrame(s, 1, "load_metadata", map[string]string{"source": "explicit"})
	if _, ok := s.phase(frame); !ok {
		t.Fatal("initial phase rejected")
	}
	if _, ok := s.phase(frame); ok || native.calls != 1 {
		t.Fatal("duplicate/replay executed")
	}
}

func TestBundleRejectsUnboundPhaseAndUnknownChild(t *testing.T) {
	native := &bundlePhaseFixture{}
	s := newBundleExchange(ProjectBundleRequest{Kind: "disk", Source: "s", Destination: "d"}, native)
	if _, ok := s.phase(bundleFixtureFrame(s, 1, "remove_recipe", map[string]interface{}{})); ok || native.calls != 0 {
		t.Fatal("out-of-order write accepted")
	}
	if _, ok := s.phase(bundleFixtureFrame(s, 1, "load_metadata", map[string]string{"source": "global"})); ok || native.calls != 0 {
		t.Fatal("unbound source accepted")
	}
	// Existing test-binary child dispatch rejects this interpreter argv. A real
	// abnormal direct child must stay UNKNOWN and expose no retained error.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINGTAI_PROJECT_DRIVER_FIXTURE", "bundle_unsupported")
	r, err := MaterializeProjectRecipe(executable, s.request, native)
	var unknown *ProjectBundleUnknownError
	if !errors.As(err, &unknown) || r.Original != nil || native.calls != 0 {
		t.Fatal("abnormal child became a known result")
	}
}

// Specialized executable child fixture, handled before the existing TestMain;
// it accepts the real interpreter argv and exchanges a single native failure.
func init() {
	mode := os.Getenv("LINGTAI_BUNDLE_DRIVER_FIXTURE")
	if mode == "" {
		return
	}
	if len(os.Args) != 5 || os.Args[1] != "-m" || os.Args[2] != "lingtai" || os.Args[3] != "project" || os.Args[4] != "recipe-materialize" {
		os.Exit(2)
	}
	decoder := json.NewDecoder(os.Stdin)
	var binding map[string]string
	if decoder.Decode(&binding) != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"status": "bundle_io", "binding": binding, "sequence": 1, "operation": "load_metadata", "arguments": map[string]string{"source": "explicit"}})
	var ack struct {
		Status    string          `json:"status"`
		Sequence  int             `json:"sequence"`
		Operation string          `json:"operation"`
		Result    json.RawMessage `json:"result"`
	}
	if decoder.Decode(&ack) != nil || ack.Status != "bundle_io_result" || ack.Sequence != 1 || ack.Operation != "load_metadata" {
		os.Exit(2)
	}
	rows := []projectBundleReceipt{{ack.Sequence, ack.Operation, ack.Result}}
	if mode == "forged" {
		rows = []projectBundleReceipt{}
	}
	terminal := map[string]interface{}{"status": "bundle_materialized", "binding": binding, "source": "explicit", "outcome": "failed", "code": "source_invalid", "failure": ProjectBundleFailure{1, "load_metadata"}, "transcript": rows}
	_ = json.NewEncoder(os.Stdout).Encode(terminal)
	if mode == "extra" {
		_ = json.NewEncoder(os.Stdout).Encode(terminal)
	}
	if mode == "stderr" {
		fmt.Fprintln(os.Stderr, "unexpected diagnostic")
	}
	os.Exit(0)
}

func TestBundleLiveChildTerminalBeforeNativeErrorExposure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "forged", "extra", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("LINGTAI_BUNDLE_DRIVER_FIXTURE", mode)
			original := &os.PathError{Op: "read", Path: "native", Err: os.ErrPermission}
			native := &bundlePhaseFixture{failure: original}
			result, err := MaterializeProjectRecipe(executable, ProjectBundleRequest{Kind: "disk", Source: "s", Destination: "d"}, native)
			if native.calls != 1 {
				t.Fatal("native phase not once-only")
			}
			if mode == "valid" {
				if err != nil || result.Original != original {
					t.Fatalf("native error identity lost: %v", err)
				}
			} else {
				var unknown *ProjectBundleUnknownError
				if !errors.As(err, &unknown) || result.Original != nil {
					t.Fatal("unvalidated transport exposed native error")
				}
			}
		})
	}
}
