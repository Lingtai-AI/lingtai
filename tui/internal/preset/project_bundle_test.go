package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleInstalledOriginalDiskCopyAndMetadata(t *testing.T) {
	fixtureRecipeRuntime(t)
	library := "library"
	source := makeBundleRoot(t, "native", &library)
	destination := t.TempDir()
	recipeFixtureWrite(t, filepath.Join(destination, ".recipe", "old"), []byte("replace"))
	recipeFixtureWrite(t, filepath.Join(destination, library, "keep"), []byte("keep"))
	recipeFixtureWrite(t, filepath.Join(source, library, "new"), []byte{0, 0xff})
	recipeFixtureWrite(t, filepath.Join(source, ".lingtai", "worker", "init.json"), []byte("imported"))
	recipeFixtureWrite(t, filepath.Join(source, "not-a-bundle-member"), []byte("exclude"))
	if err := CopyBundle(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".recipe", "old")); !os.IsNotExist(err) {
		t.Fatal("recipe not replaced")
	}
	recipeFixtureBytes(t, filepath.Join(destination, library, "keep"), []byte("keep"))
	recipeFixtureBytes(t, filepath.Join(destination, library, "new"), []byte{0, 0xff})
	recipeFixtureBytes(t, filepath.Join(destination, ".lingtai", "worker", "init.json"), []byte("imported"))
	if _, err := os.Stat(filepath.Join(destination, "not-a-bundle-member")); !os.IsNotExist(err) {
		t.Fatal("unrelated sibling copied")
	}
	// Source validation must precede same-Abs noop; the original native decoder
	// error survives a duplicate later-valid name, not a Python approximation.
	recipeFixtureWrite(t, filepath.Join(source, ".recipe", "recipe.json"), []byte(`{"name":7,"name":"valid"}`))
	err := CopyBundle(source, source)
	var typed *json.UnmarshalTypeError
	if !errors.As(err, &typed) || typed.Field != "name" || !strings.HasPrefix(err.Error(), "CopyBundle: source bundle invalid: parse recipe.json: ") {
		t.Fatalf("same-path metadata error: %v", err)
	}
}

func TestBundleInstalledPackagedColdOperationalBytes(t *testing.T) {
	fixtureRecipeRuntime(t)
	for _, name := range []string{"adaptive", "greeter", "plain", "tutorial"} {
		t.Run(name, func(t *testing.T) {
			destination := t.TempDir()
			if err := CopyEmbeddedBundle(name, destination); err != nil {
				t.Fatal(err)
			}
			root := embeddedRecipeRoot(name) // test oracle ONLY; production never reads Go assets
			err := iofs.WalkDir(recipeAssetsFS, root, func(member string, entry iofs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				want, err := recipeAssetsFS.ReadFile(member)
				if err != nil {
					return err
				}
				recipeFixtureBytes(t, filepath.Join(destination, filepath.FromSlash(strings.TrimPrefix(member, root+"/"))), want)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	// Lexical parent selection is NOT "not found": native missing io/fs member
	// and its old wrapper must remain in the chain.
	err := CopyEmbeddedBundle(".", t.TempDir())
	var missing *iofs.PathError
	if !errors.As(err, &missing) || missing.Op != "open" || !errors.Is(err, iofs.ErrNotExist) || !strings.HasPrefix(err.Error(), "CopyEmbeddedBundle: source bundle invalid: read embedded recipe.json: ") {
		t.Fatalf("lexical packaged metadata: %v", err)
	}
}

func TestBundleInstalledWizardLocalAndMalformedGlobal(t *testing.T) {
	fixtureRecipeRuntime(t)
	local := makeBundleRoot(t, "local-authority", nil)
	metadata, err := os.ReadFile(filepath.Join(local, ".recipe", "recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := StageRecipeBundle(filepath.Join(local, ".lingtai"), t.TempDir(), "")
	if err != nil || root != local {
		t.Fatalf("valid local empty-name choice: %s %v", root, err)
	}
	recipeFixtureBytes(t, filepath.Join(local, ".recipe", "recipe.json"), metadata)
	stray, global := t.TempDir(), t.TempDir()
	recipeFixtureWrite(t, filepath.Join(stray, ".recipe", "recipe.json"), []byte("malformed-local"))
	recipeFixtureWrite(t, filepath.Join(global, "recipes", "custom-category", "adaptive", ".recipe", "recipe.json"), []byte(`{"name":false}`))
	_, err = StageRecipeBundle(filepath.Join(stray, ".lingtai"), global, "adaptive")
	var typed *json.UnmarshalTypeError
	if !errors.As(err, &typed) || !strings.HasPrefix(err.Error(), "copyRecipeBundle: CopyBundle: source bundle invalid: parse recipe.json: ") {
		t.Fatalf("malformed global fell back: %v", err)
	}
	recipeFixtureBytes(t, filepath.Join(stray, ".recipe", "recipe.json"), []byte("malformed-local"))
	// No global candidate: malformed local is replaced by packaged plain.
	_, err = StageRecipeBundle(filepath.Join(stray, ".lingtai"), t.TempDir(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	info, err := LoadRecipeInfo(stray, "")
	if err != nil || info.ID != "plain" {
		t.Fatalf("packaged stage: %+v %v", info, err)
	}
}

func TestBundleNativeDecoderLegacyDepthAndErrorDomain(t *testing.T) {
	deep := strings.Repeat("[", 9999) + "0" + strings.Repeat("]", 9999)
	for _, data := range [][]byte{
		[]byte(`{"name":"valid","extension":` + deep + `}`),
		[]byte(`{"name":` + deep + `}`),
		[]byte(`{"name":7,"name":"valid"}`),
	} {
		got, gotErr := recipeBundleDecode(data)
		want, wantErr := decodeRecipeInfo(data, "fixture")
		if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) || (gotErr == nil && got.Name != want.Name) {
			t.Fatalf("native decoder domain changed: %v / %v", gotErr, wantErr)
		}
	}
	member := recipeBundleMemberFS{name: "member", content: []byte{}}
	got, err := iofs.ReadFile(member, "member")
	if err != nil || !bytes.Equal(got, []byte{}) {
		t.Fatal("empty supplied member became absent")
	}
}
