package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Depth counts include the top-level object, matching the supplied 9999/10000
// boundary. This exercises the full original decoder, not a hand-projection.
func nativeRecipeNested(depth int) string {
	return strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
}

func TestNativeRecipeMetadataUsesOriginalTypedDecoder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".recipe", "recipe.json")
	for _, depth := range []int{2000, 9999, 10000} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			raw := []byte(`{"NAME":"fixture","unknown":` + nativeRecipeNested(depth) + `,"library_name":"lib"}`)
			recipeFixtureWrite(t, path, raw)
			var original RecipeInfo
			oldErr := json.Unmarshal(raw, &original)
			native := newNativeRecipeOwner(root, "")
			observed, failed := native.Metadata()
			if (oldErr != nil) != (failed != nil) {
				t.Fatalf("native/original depth boundary differs: %v %#v", oldErr, failed)
			}
			if depth < 10000 && failed != nil || depth == 10000 && failed == nil {
				t.Fatal("established depth acceptance changed")
			}
			if failed == nil && (observed.Name != original.Name || observed.LibraryName == nil || *observed.LibraryName != "lib") {
				t.Fatal("native typed metadata differs")
			}
			recipeFixtureBytes(t, path, raw)
		})
	}
	for _, raw := range []string{`{"name":42,"name":"valid"}`, `{"name":"valid","description":42}`, `{"name":"valid","VERSION":42}`} {
		recipeFixtureWrite(t, path, []byte(raw))
		native := newNativeRecipeOwner(root, "")
		_, failed := native.Metadata()
		var typed *json.UnmarshalTypeError
		if failed == nil || !errors.As(native.NativeError(failed), &typed) {
			t.Fatal("original decoder error identity lost")
		}
	}
	recipeFixtureWrite(t, path, []byte("{\"name\":\"x\xff\",\"NAME\":null,\"library_name\":\"lib\",\"LIBRARY_NAME\":null}"))
	got, failed := newNativeRecipeOwner(root, "").Metadata()
	if failed != nil || got.Name != "x\ufffd" || got.LibraryName != nil {
		t.Fatal("null/case/invalid UTF8 mismatch")
	}
}

func TestNativeRecipeOpaqueInitExactOldWriteBytes(t *testing.T) {
	for _, depth := range []int{2000, 9999, 10000} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			root := t.TempDir()
			oldPath, newPath := filepath.Join(root, "old.json"), filepath.Join(root, "worker", "init.json")
			raw := []byte(`{"unknown":` + nativeRecipeNested(depth) + `,"big":9007199254740993,"manifest":{"capabilities":{"skills":{"paths":[{"opaque":[1,2]},"old"]}}}}`)
			recipeFixtureWrite(t, oldPath, raw)
			recipeFixtureWrite(t, newPath, raw)
			oldErr := AppendSkillsPath(oldPath, "../../lib")
			native := newNativeRecipeOwner("", root)
			observed, failed := native.ObserveSkills("worker")
			if depth == 10000 {
				if oldErr == nil || failed == nil {
					t.Fatal("invalid depth accepted")
				}
				recipeFixtureBytes(t, newPath, raw)
				return
			}
			if oldErr != nil || failed != nil || !observed.Present || len(observed.Paths) != 2 || observed.Paths[0] != 0 || observed.Paths[1] != "old" {
				t.Fatalf("native path evidence mismatch: %v %#v", oldErr, observed)
			}
			if failure := native.WriteSkills("worker", observed.Handle, append(observed.Paths, "../../lib")); failure != nil {
				t.Fatal(native.NativeError(failure))
			}
			want, err := os.ReadFile(oldPath)
			if err != nil {
				t.Fatal(err)
			}
			recipeFixtureBytes(t, newPath, want)
			if !bytes.Contains(want, []byte(`9007199254740993`)) {
				t.Fatal("exact number lost")
			}
		})
	}
}

func TestNativeRecipeOriginalConsumerDeepMetadataAndInit(t *testing.T) {
	fixtureRecipeRuntime(t) // actual installed CLI, no source import/shim
	for _, depth := range []int{2000, 9999, 10000} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			root := t.TempDir()
			recipeFixtureWrite(t, filepath.Join(root, ".recipe", "recipe.json"), []byte(`{"name":"fixture","library_name":"lib","unknown":`+nativeRecipeNested(depth)+`}`))
			template := []byte{'G', 0, 0xff}
			recipeFixtureWrite(t, filepath.Join(root, ".recipe", "greet", "greet.md"), template)
			for _, member := range []string{"a", "b"} {
				recipeFixtureWrite(t, filepath.Join(root, ".lingtai", member, "init.json"), []byte(`{"unknown":`+nativeRecipeNested(depth)+`,"big":9007199254740993,"manifest":{"capabilities":{"skills":{"paths":[]}}}}`))
			}
			calls := 0
			count, err := ApplyRecipe(root, "", func(raw string) string {
				if raw != string(template) {
					t.Fatal("template byte change")
				}
				if calls > 0 {
					previous, err := os.ReadFile(filepath.Join(root, ".lingtai", "a", "init.json"))
					if err != nil || !bytes.Contains(previous, []byte(`"../../lib"`)) {
						t.Fatal("callback preceded previous path write")
					}
				}
				calls++
				return "" // original nonempty template still truncates prompt
			})
			if depth == 10000 {
				var syntax *json.SyntaxError
				if !errors.As(err, &syntax) || count != 0 || calls != 0 {
					t.Fatalf("original native metadata error not restored: %d %d %v", count, calls, err)
				}
				recipeFixtureNoSnapshot(t, filepath.Join(root, ".lingtai"))
				return
			}
			if err != nil || count != 2 || calls != 2 {
				t.Fatalf("original consumer count/callbacks: %d %d %v", count, calls, err)
			}
			for _, member := range []string{"a", "b"} {
				recipeFixtureBytes(t, filepath.Join(root, ".lingtai", member, ".prompt"), []byte{})
			}
			recipeFixtureBytes(t, filepath.Join(root, ".lingtai", AppliedRecipeSubpath, "greet", "greet.md"), template)
		})
	}
}

func TestNativeRecipeLiveLayerWrappersSelectUnreadableLocale(t *testing.T) {
	fixtureRecipeRuntime(t)
	root := t.TempDir()
	for _, layer := range []string{"comment", "covenant"} {
		locale := filepath.Join(root, ".recipe", layer, "zh", layer+".md")
		recipeFixtureWrite(t, locale, []byte("locale"))
		recipeFixtureWrite(t, filepath.Join(root, ".recipe", layer, layer+".md"), []byte("root readable"))
		if err := os.Chmod(locale, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(locale, 0o644)
		selected := ResolveCommentPath(root, "zh")
		if layer == "covenant" {
			selected = ResolveCovenantPath(root, "zh")
		}
		if selected != locale {
			t.Fatalf("readability changed first non-dir selection: %s", selected)
		}
	}
}

func TestNativeRecipeDeepNonStringPathRemainsOpaque(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "old.json"), filepath.Join(root, "worker", "init.json")
	raw := []byte(`{"manifest":{"capabilities":{"skills":{"paths":[` + nativeRecipeNested(9994) + `,"old"]}}}}`)
	recipeFixtureWrite(t, oldPath, raw)
	recipeFixtureWrite(t, newPath, raw)
	if err := AppendSkillsPath(oldPath, "../../lib"); err != nil {
		t.Fatal(err)
	}
	native := newNativeRecipeOwner("", root)
	observed, failed := native.ObserveSkills("worker")
	if failed != nil || len(observed.Paths) != 2 || observed.Paths[0] != 0 {
		t.Fatal("deep non-string entry escaped its opaque identity")
	}
	if failed := native.WriteSkills("worker", observed.Handle, append(observed.Paths, "../../lib")); failed != nil {
		t.Fatal(native.NativeError(failed))
	}
	want, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	recipeFixtureBytes(t, newPath, want)
}
