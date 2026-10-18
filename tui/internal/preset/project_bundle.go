package preset

import (
	"bytes"
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/anthropics/lingtai-tui/internal/process"
)

// recipeBundleNative is bound to ORIGINAL native strings. It executes only
// individual Core-requested phases; it never runs CopyBundle, selects fallback,
// rechecks metadata, or substitutes Python path/error/copy behavior.
type recipeBundleNative struct {
	request                  process.ProjectBundleRequest
	globalRoot, packagedRoot string
	library                  map[string]*string
}

func newRecipeBundleNative(request process.ProjectBundleRequest) *recipeBundleNative {
	return &recipeBundleNative{request: request, library: map[string]*string{}}
}
func (n *recipeBundleNative) source(source string) string {
	switch source {
	case "explicit":
		return n.request.Source
	case "local":
		return n.request.Destination
	case "global":
		return n.globalRoot
	case "packaged":
		return n.packagedRoot
	}
	panic("unbound Recipe bundle source") // closed transport must reject before I/O
}
func recipeBundleDirectory(name string) string {
	st, err := os.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return "absent"
		}
		return "stat_failed"
	}
	if st.IsDir() {
		return "directory"
	}
	return "not_directory"
}
func (n *recipeBundleNative) ObserveLocalRecipe() string {
	return recipeBundleDirectory(filepath.Join(n.request.Destination, RecipeDotDir))
}
func (n *recipeBundleNative) FindGlobal() bool {
	n.globalRoot = RecipeDir(n.request.GlobalDir, n.request.RecipeName)
	return n.globalRoot != ""
}

// Full legacy struct decoding is essential: even an earlier wrong-typed
// duplicate followed by a valid value retains its original Go decoder error.
// Core, not this native phase, decides whether the decoded Name is empty.
func recipeBundleDecode(data []byte) (process.ProjectBundleMetadata, error) {
	var info RecipeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return process.ProjectBundleMetadata{}, fmt.Errorf("parse recipe.json: %w", err)
	}
	return process.ProjectBundleMetadata{Name: info.Name, Library: info.LibraryName}, nil
}
func (n *recipeBundleNative) LoadMetadata(source string) (process.ProjectBundleMetadata, error) {
	root := n.source(source)
	if root == "" {
		return process.ProjectBundleMetadata{}, fmt.Errorf("empty recipe bundle directory")
	}
	name := filepath.Join(root, RecipeDotDir, "recipe.json")
	if info, err := os.Stat(name); err != nil || info.IsDir() {
		return process.ProjectBundleMetadata{}, fmt.Errorf(".recipe/recipe.json not found in %s", root)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return process.ProjectBundleMetadata{}, fmt.Errorf("read recipe.json: %w", err)
	}
	metadata, err := recipeBundleDecode(data)
	if err == nil {
		n.library[source] = metadata.Library
	}
	return metadata, err
}
func (n *recipeBundleNative) AbsoluteLocations(source string) (*string, *string) {
	a, ae := filepath.Abs(n.source(source))
	b, be := filepath.Abs(n.request.Destination)
	var ap, bp *string
	if ae == nil {
		ap = &a
	}
	if be == nil {
		bp = &b
	}
	return ap, bp
}
func (n *recipeBundleNative) RemoveRecipe() error {
	err := os.RemoveAll(filepath.Join(n.request.Destination, RecipeDotDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func (n *recipeBundleNative) CopyRecipe(source string) error {
	return copyTree(filepath.Join(n.source(source), RecipeDotDir), filepath.Join(n.request.Destination, RecipeDotDir))
}
func (n *recipeBundleNative) ObserveLibrary(source, library string) string {
	return recipeBundleDirectory(filepath.Join(n.source(source), library))
}
func (n *recipeBundleNative) CopyLibrary(source, library string) error {
	return copyTree(filepath.Join(n.source(source), library), filepath.Join(n.request.Destination, library))
}
func (n *recipeBundleNative) ObserveNetwork(source string) string {
	return recipeBundleDirectory(filepath.Join(n.source(source), ".lingtai"))
}
func (n *recipeBundleNative) CopyNetwork(source string) error {
	return copyTree(filepath.Join(n.source(source), ".lingtai"), filepath.Join(n.request.Destination, ".lingtai"))
}
func (n *recipeBundleNative) PackagedCandidates() []string {
	// Preserve the original slash path.Join lexical oddities, including names
	// "." and "../..". No disk/old embed lookup and no resource prefetch.
	return []string{
		path.Join("recipe_assets", "recommended", n.request.RecipeName),
		path.Join("recipe_assets", "intrinsic", n.request.RecipeName),
		path.Join("recipe_assets", "examples", n.request.RecipeName),
	}
}

// recipeBundleMemberFS realizes ONLY the supplied kernel member using native
// io/fs ReadFile semantics. Nil is statically missing, not an empty file. The
// original compiled assets remain intact and are never consulted by this driver.
type recipeBundleMemberFS struct {
	name    string
	content []byte
}

func (f recipeBundleMemberFS) Open(name string) (iofs.File, error) {
	if name != f.name || f.content == nil {
		return nil, &iofs.PathError{Op: "open", Path: name, Err: iofs.ErrNotExist}
	}
	return &recipeBundleMemberFile{Reader: bytes.NewReader(f.content), name: path.Base(name), size: int64(len(f.content))}, nil
}

type recipeBundleMemberFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *recipeBundleMemberFile) Close() error { return nil }
func (f *recipeBundleMemberFile) Stat() (iofs.FileInfo, error) {
	return recipeBundleMemberInfo{f.name, f.size}, nil
}

type recipeBundleMemberInfo struct {
	name string
	size int64
}

func (f recipeBundleMemberInfo) Name() string        { return f.name }
func (f recipeBundleMemberInfo) Size() int64         { return f.size }
func (f recipeBundleMemberInfo) Mode() iofs.FileMode { return 0o444 }
func (f recipeBundleMemberInfo) ModTime() time.Time  { return time.Time{} }
func (f recipeBundleMemberInfo) IsDir() bool         { return false }
func (f recipeBundleMemberInfo) Sys() interface{}    { return nil }
func (n *recipeBundleNative) DecodePackagedMetadata(root string, content []byte) (process.ProjectBundleMetadata, error) {
	n.packagedRoot = root
	name := path.Join(root, RecipeDotDir, "recipe.json")
	data, err := iofs.ReadFile(recipeBundleMemberFS{name, content}, name)
	if err != nil {
		return process.ProjectBundleMetadata{}, fmt.Errorf("read embedded recipe.json: %w", err)
	}
	metadata, err := recipeBundleDecode(data)
	if err == nil {
		n.library["packaged"] = metadata.Library
	}
	return metadata, err
}
func (n *recipeBundleNative) PreparePackagedFile(relative string) error {
	target := filepath.Join(n.request.Destination, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	return nil
}
func (n *recipeBundleNative) WritePackagedFile(relative string, content []byte) error {
	target := filepath.Join(n.request.Destination, filepath.FromSlash(relative))
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func materializeRecipeBundle(request process.ProjectBundleRequest) error {
	python, err := recipePython() // existing caller's interpreter selection only
	if err != nil {
		return err
	}
	native := newRecipeBundleNative(request)
	result, err := process.MaterializeProjectRecipe(python, request, native)
	if err != nil {
		return err
	} // UNKNOWN is never a native/domain failure
	prefix := "CopyBundle"
	if result.Source == "packaged" {
		prefix = "CopyEmbeddedBundle"
	}
	switch result.Code {
	case "":
		return nil
	case "empty_paths":
		return fmt.Errorf("%s: empty paths", prefix)
	case "packaged_not_found":
		return fmt.Errorf("CopyEmbeddedBundle: recipe %q not found", request.RecipeName)
	case "source_empty_name":
		return fmt.Errorf("%s: source bundle invalid: %w", prefix, fmt.Errorf("recipe.json has empty name in %s", native.source(result.Source)))
	case "source_invalid":
		return fmt.Errorf("%s: source bundle invalid: %w", prefix, result.Original)
	case "remove_recipe_failed":
		return fmt.Errorf("%s: remove old .recipe/: %w", prefix, result.Original)
	case "copy_recipe_failed":
		return fmt.Errorf("CopyBundle: copy .recipe/: %w", result.Original)
	case "copy_library_failed":
		return fmt.Errorf("CopyBundle: copy library %q: %w", *native.library[result.Source], result.Original)
	case "copy_network_failed":
		return fmt.Errorf("CopyBundle: copy .lingtai/: %w", result.Original)
	case "packaged_create_failed", "packaged_write_failed":
		return result.Original
	}
	return &process.ProjectBundleUnknownError{}
}

// StageRecipeBundle is the wizard's bound native entry. Core owns the ORIGINAL
// local-valid/global-found/packaged decision, including valid-local wins even
// for an empty requested name. Keep setup/draft/import guards at their callers.
func StageRecipeBundle(lingtaiDir, globalDir, recipeName string) (projectRoot string, err error) {
	projectRoot = filepath.Dir(lingtaiDir)
	err = materializeRecipeBundle(process.ProjectBundleRequest{Kind: "wizard", Destination: projectRoot, RecipeName: recipeName, GlobalDir: globalDir})
	if err != nil {
		err = fmt.Errorf("copyRecipeBundle: %w", err)
	}
	return projectRoot, err
}

var _ process.ProjectBundleNative = (*recipeBundleNative)(nil)
