package preset

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// Test-only selection via the existing configuration fixture's explicit cold
// interpreter. The parent's RECIPE-COLD-0143 venv is linked read-only into a
// disposable HOME, so ORIGINAL public APIs exercise the real installed CLI.
// No product environment flag, process-global function override or source import.
func fixtureRecipeRuntime(t *testing.T) {
	t.Helper()
	python := fixtureProjectPython(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	venv := filepath.Join(home, config.GlobalDirName, "runtime", "venv")
	if err := os.MkdirAll(filepath.Dir(venv), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(filepath.Dir(python)), venv); err != nil {
		t.Fatalf("fixture installed runtime link: %v (native fixture support required)", err)
	}
	selected, err := recipePython()
	if err != nil || selected != config.VenvPython(venv) {
		t.Fatalf("wrong installed runtime selection: %s %v", selected, err)
	}
}

func recipeFixtureWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func recipeFixtureBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("fixture byte mismatch at %s: got length=%d want length=%d err=%v", filepath.Base(path), len(got), len(want), err)
	}
}
func recipeFixtureNoSnapshot(t *testing.T, destination string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(destination, AppliedRecipeSubpath)); !os.IsNotExist(err) {
		t.Fatalf("snapshot after aborted/pretraversal operation: %v", err)
	}
}

// 1. Original arbitrary function / causal ordering / explicit staging / growing
// binary input+output. Callback edits future init/tree/greet; enumeration and
// original raw template remain captured once while future init is read late.
func TestRecipeExchangeInstalledSequentialBytesAndDestination(t *testing.T) {
	fixtureRecipeRuntime(t)
	for _, canonical := range []bool{false, true} {
		t.Run(map[bool]string{false: "staging", true: "canonical"}[canonical], func(t *testing.T) {
			lib := "library"
			bundle := makeBundleRoot(t, "exchange", &lib)
			destination := t.TempDir()
			if canonical {
				destination = filepath.Join(bundle, ".lingtai")
			}
			template := bytes.Repeat([]byte{'G', 0, 0xff}, (1<<20)/3+37)
			recipeFixtureWrite(t, filepath.Join(bundle, ".recipe", "greet", "greet.md"), template)
			for _, name := range []string{"a", "b", "c", "human", ".hidden"} {
				recipeFixtureWrite(t, filepath.Join(destination, name, ".prompt"), []byte("old"))
				recipeFixtureWrite(t, filepath.Join(destination, name, "init.json"), []byte(`{"extension":9007199254740993,"manifest":{"capabilities":{"skills":{"paths":[]}}}}`))
			}
			recipeFixtureWrite(t, filepath.Join(destination, "file"), []byte("not directory"))
			if err := os.Symlink(filepath.Join(destination, "a"), filepath.Join(destination, "link")); err != nil {
				t.Fatal(err)
			}
			outputs := [][]byte{{}, {0, 0xff, 'B'}, bytes.Repeat([]byte{0xfe, 0, 'C'}, (1<<20)/3+71)}
			calls := 0
			callback := func(raw string) string {
				if !bytes.Equal([]byte(raw), template) {
					t.Fatal("template reread/text conversion")
				}
				index := calls
				calls++
				if index >= 3 {
					t.Fatal("unexpected eligible member / callback replay")
				}
				current := []string{"a", "b", "c"}[index]
				recipeFixtureBytes(t, filepath.Join(destination, current, ".prompt"), []byte("old"))
				if index > 0 {
					previous := []string{"a", "b", "c"}[index-1]
					recipeFixtureBytes(t, filepath.Join(destination, previous, ".prompt"), outputs[index-1])
					paths := skillsPaths(t, filepath.Join(destination, previous, "init.json"))
					if len(paths) != 1 || paths[0] != "../../library" {
						t.Fatal("next callback preceded prior skills write")
					}
				}
				if index == 0 {
					recipeFixtureWrite(t, filepath.Join(bundle, ".recipe", "greet", "greet.md"), []byte("changed after capture"))
					recipeFixtureWrite(t, filepath.Join(destination, "d", ".prompt"), []byte("not enumerated"))
					recipeFixtureWrite(t, filepath.Join(destination, "b", "init.json"), []byte(`{"callback_side_effect":true,"manifest":{"capabilities":{"skills":{"paths":[]}}}}`))
				}
				return string(outputs[index])
			}
			var count int
			var err error
			if canonical {
				count, err = ApplyRecipe(bundle, "", callback)
			} else {
				count, err = ApplyRecipeToLingTaiDir(bundle, destination, "", callback)
			}
			if err != nil || count != 3 || calls != 3 {
				t.Fatalf("count=%d calls=%d err=%v", count, calls, err)
			}
			for i, name := range []string{"a", "b", "c"} {
				recipeFixtureBytes(t, filepath.Join(destination, name, ".prompt"), outputs[i])
			}
			for _, name := range []string{"human", ".hidden"} {
				recipeFixtureBytes(t, filepath.Join(destination, name, ".prompt"), []byte("old"))
			}
			recipeFixtureBytes(t, filepath.Join(destination, "d", ".prompt"), []byte("not enumerated"))
			data, err := os.ReadFile(filepath.Join(destination, "b", "init.json"))
			if err != nil || !bytes.Contains(data, []byte(`"callback_side_effect":true`)) {
				t.Fatal("late init observation lost function side effect")
			}
			recipeFixtureBytes(t, filepath.Join(destination, AppliedRecipeSubpath, "greet", "greet.md"), []byte("changed after capture"))
			if !canonical {
				recipeFixtureNoSnapshot(t, filepath.Join(bundle, ".lingtai"))
			} else if RecipeNeedsApply(bundle) {
				t.Fatal("snapshot comparison not through kernel")
			}
		})
	}
}

// 2. Nil identity and original absent/empty layers have no callbacks. Kernel
// owns selection/byte comparison; non-directory selected recipe remains absent.
func TestRecipeExchangeInstalledIdentityAndNoCallbacks(t *testing.T) {
	fixtureRecipeRuntime(t)
	for _, mode := range []string{"identity", "empty", "missing", "no_members"} {
		t.Run(mode, func(t *testing.T) {
			bundle := makeBundleRoot(t, mode, nil)
			dest := filepath.Join(bundle, ".lingtai")
			recipeFixtureWrite(t, filepath.Join(dest, "a", ".prompt"), []byte("old"))
			greet := filepath.Join(bundle, ".recipe", "greet", "greet.md")
			raw := []byte{0, 0xff, 'x'}
			recipeFixtureWrite(t, greet, raw)
			callback := func(string) string { t.Fatal("unexpected callback for absent original greet/member"); return "" }
			wantCount := 1
			switch mode {
			case "identity":
				callback = nil
			case "empty":
				recipeFixtureWrite(t, greet, nil)
			case "missing":
				if err := os.Remove(greet); err != nil {
					t.Fatal(err)
				}
			case "no_members":
				if err := os.Rename(filepath.Join(dest, "a"), filepath.Join(dest, ".a")); err != nil {
					t.Fatal(err)
				}
				wantCount = 0
			}
			count, err := ApplyRecipe(bundle, "", callback)
			if count != wantCount || err != nil {
				t.Fatalf("count=%d err=%v", count, err)
			}
			if mode == "identity" {
				recipeFixtureBytes(t, filepath.Join(dest, "a", ".prompt"), raw)
			} else if mode != "no_members" {
				recipeFixtureBytes(t, filepath.Join(dest, "a", ".prompt"), []byte("old"))
			}
			if RecipeNeedsApply(bundle) {
				t.Fatal("normal snapshot should compare equal")
			}
		})
	}
	absent := t.TempDir()
	if RecipeNeedsApply(absent) || RecipeNeedsApply("") {
		t.Fatal("absence boolean changed")
	}
	recipeFixtureWrite(t, filepath.Join(absent, ".recipe"), []byte("not directory"))
	if RecipeNeedsApply(absent) {
		t.Fatal("non-directory recipe is not absent")
	}
}

// 3. Actual installed metadata/enumeration failures occur before callbacks and
// snapshot; factual typed evidence/errno is retained without decoder guessing.
func TestRecipeExchangeInstalledPretraversalErrors(t *testing.T) {
	fixtureRecipeRuntime(t)
	for _, mode := range []string{"enumeration", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			bundle := makeBundleRoot(t, mode, nil)
			dest := filepath.Join(t.TempDir(), "missing")
			if mode == "metadata" {
				recipeFixtureWrite(t, filepath.Join(bundle, ".recipe", "recipe.json"), []byte(`{"name":`))
			}
			count, err := ApplyRecipeToLingTaiDir(bundle, dest, "", func(string) string { t.Fatal("callback before preflight failure"); return "" })
			var failure *process.ProjectRecipeError
			if count != 0 || !errors.As(err, &failure) || !failure.Known || failure.Result.LegacyFailure == nil {
				t.Fatalf("lost known pretraversal evidence: count=%d err=%v", count, err)
			}
			if failure.Result.LegacyFailure.Phase != mode {
				t.Fatalf("wrong first phase: %+v", failure.Result.LegacyFailure)
			}
			if mode == "enumeration" && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("errno chain lost")
			}
			if mode == "metadata" && failure.Result.LegacyFailure.ParseOffset == nil {
				t.Fatal("proved parse byte offset lost")
			}
			recipeFixtureNoSnapshot(t, dest)
		})
	}
}

// 4. Old first-error/count quirk, reader-only diagnostic nil, and snapshot
// meaning remain distinct. Later errors never replace the first prompt failure.
func TestRecipeExchangeInstalledFirstErrorAndReaderDiagnostic(t *testing.T) {
	fixtureRecipeRuntime(t)
	lib := "library"
	bundle := makeBundleRoot(t, "first", &lib)
	dest := filepath.Join(bundle, ".lingtai")
	for _, name := range []string{"a", "b", "c"} {
		recipeFixtureWrite(t, filepath.Join(dest, name, "init.json"), []byte(`{"manifest":{"capabilities":{"skills":{"paths":[]}}}}`))
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(dest, name, ".prompt"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	recipeFixtureWrite(t, filepath.Join(dest, "c", "init.json"), []byte(`{"invalid":`))
	calls := 0
	count, err := ApplyRecipe(bundle, "", func(raw string) string { calls++; return raw })
	var failure *process.ProjectRecipeError
	if count != 2 || calls != 3 || !errors.As(err, &failure) || !failure.Known || failure.Result.LegacyFailure.Phase != "prompt" {
		t.Fatalf("first-error count/phase changed: %d %d %v", count, calls, err)
	}
	var native *os.PathError
	if !errors.As(err, &native) || native.Path != filepath.Join(dest, "a", ".prompt") {
		t.Fatal("first factual native path lost")
	}
	if !strings.Contains(err.Error(), "write .prompt for a") {
		t.Fatal("first phase prefix lost")
	}
	if len(skillsPaths(t, filepath.Join(dest, "a", "init.json"))) != 0 {
		t.Fatal("first prompt failure did not skip skills")
	}
	if len(skillsPaths(t, filepath.Join(dest, "b", "init.json"))) != 1 {
		t.Fatal("later prompt failure incorrectly skipped skills")
	}
	if RecipeNeedsApply(bundle) {
		t.Fatal("partial normal traversal must still snapshot")
	}

	readerBundle := makeBundleRoot(t, "reader", &lib)
	readerDest := filepath.Join(readerBundle, ".lingtai")
	recipeFixtureWrite(t, filepath.Join(readerDest, "a", "init.json"), []byte(`{"manifest":{"capabilities":{"skills":{"paths":[]}}}}`))
	python, err := recipePython()
	if err != nil {
		t.Fatal(err)
	}
	result, err := process.ApplyProjectRecipe(python, readerBundle, readerDest, "", nil)
	if err != nil || result.LegacyApplied != 1 || result.Outcome != "partial" || result.Members[0].ReaderStatus != "READ_FAILED" || !result.Members[0].InitWritten {
		t.Fatalf("reader diagnostic became old error: %+v %v", result, err)
	}
	for _, firstPrompt := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot_first_error", true: "prompt_precedes_snapshot"}[firstPrompt], func(t *testing.T) {
			bundle := makeBundleRoot(t, "snapshot", nil)
			dest := filepath.Join(bundle, ".lingtai")
			recipeFixtureWrite(t, filepath.Join(dest, "a", ".prompt"), []byte("old"))
			if firstPrompt {
				if err := os.Remove(filepath.Join(dest, "a", ".prompt")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dest, "a", ".prompt"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			recipeFixtureWrite(t, filepath.Join(dest, ".tui-asset"), []byte("blocked ancestor"))
			count, err := ApplyRecipe(bundle, "", nil)
			var failure *process.ProjectRecipeError
			wantPhase, wantCount := "snapshot", 1
			if firstPrompt {
				wantPhase, wantCount = "prompt", 0
			}
			if count != wantCount || !errors.As(err, &failure) || !failure.Known || failure.Result.LegacyFailure == nil || failure.Result.LegacyFailure.Phase != wantPhase || !failure.Result.Snapshot.Attempted || failure.Result.Snapshot.Copied {
				t.Fatalf("snapshot first-error/count evidence drift: %d %v", count, err)
			}
		})
	}
}

// 5. Panic retains ORIGINAL value and Goexit retains unwind semantics. Both
// leave prior writes, never touch pending/later members, and never snapshot.
func TestRecipeExchangeInstalledPanicAndGoexit(t *testing.T) {
	fixtureRecipeRuntime(t)
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			bundle := makeBundleRoot(t, mode, nil)
			dest := filepath.Join(bundle, ".lingtai")
			for _, name := range []string{"a", "b", "c"} {
				recipeFixtureWrite(t, filepath.Join(dest, name, ".prompt"), []byte("old"))
			}
			marker := &struct{ label string }{"original panic"}
			calls := 0
			callback := func(string) string {
				calls++
				if calls == 2 {
					recipeFixtureBytes(t, filepath.Join(dest, "a", ".prompt"), []byte("first"))
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic(marker)
				}
				if calls > 2 {
					t.Error("callback after unwind")
				}
				return "first"
			}
			if mode == "panic" {
				func() {
					defer func() {
						if recover() != marker {
							t.Error("panic value swallowed/replaced")
						}
					}()
					_, _ = ApplyRecipe(bundle, "", callback)
					t.Error("panic converted into return")
				}()
			} else {
				done := make(chan struct{})
				go func() {
					defer close(done)
					_, _ = ApplyRecipe(bundle, "", callback)
					t.Error("Goexit converted into return")
				}()
				<-done // parent's go test timeout is validation supervision only
			}
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
			for _, name := range []string{"b", "c"} {
				recipeFixtureBytes(t, filepath.Join(dest, name, ".prompt"), []byte("old"))
			}
			recipeFixtureNoSnapshot(t, dest)
		})
	}
}
