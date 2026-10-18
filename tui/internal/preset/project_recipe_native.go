package preset

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/process"
)

// One construction-bound operation, one live owner document, no registry. These
// methods supply observations and execute field writes, not apply/append policy.
type nativeRecipeOwner struct {
	bundleDir  string
	lingtaiDir string
	handle     int
	member     string
	root       map[string]interface{}
	skills     map[string]interface{}
	original   []interface{}
	causes     map[string]error
}

func newNativeRecipeOwner(bundleDir, lingtaiDir string) *nativeRecipeOwner {
	return &nativeRecipeOwner{bundleDir: bundleDir, lingtaiDir: lingtaiDir, causes: map[string]error{}}
}

func recipeFailureKey(f *process.ProjectRecipeFailure) string {
	return f.Phase + "\x00" + f.Operation + "\x00" + f.PathB64
}

func (n *nativeRecipeOwner) failure(phase, operation, path string, err error) *process.ProjectRecipeFailure {
	f := &process.ProjectRecipeFailure{Phase: phase, Operation: operation,
		PathB64: base64.StdEncoding.EncodeToString([]byte(path))}
	// Keep the original Go decoder's factual consumed-byte count; do not
	// synthesize a Python character/index offset or rebuild the native error.
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) && syntax.Offset >= 0 {
		value := syntax.Offset
		f.ParseOffset = &value
	}
	key := recipeFailureKey(f)
	if _, exists := n.causes[key]; !exists {
		n.causes[key] = err
	}
	return f
}

func (n *nativeRecipeOwner) NativeError(f *process.ProjectRecipeFailure) error {
	if f == nil {
		return nil
	}
	return n.causes[recipeFailureKey(f)]
}

func (n *nativeRecipeOwner) Metadata() (process.ProjectRecipeMetadata, *process.ProjectRecipeFailure) {
	path := filepath.Join(n.bundleDir, RecipeDotDir, "recipe.json")
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return process.ProjectRecipeMetadata{}, n.failure("metadata", "read", path,
			fmt.Errorf(".recipe/recipe.json not found in %s", n.bundleDir))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return process.ProjectRecipeMetadata{}, n.failure("metadata", "read", path, fmt.Errorf("read recipe.json: %w", err))
	}
	var info RecipeInfo
	// EXACT old typed struct decoder: unknown deep fields are consumed natively,
	// duplicate/case/null/type/UTF-8 behavior is encoding/json's, not a projection.
	if err := json.Unmarshal(data, &info); err != nil {
		return process.ProjectRecipeMetadata{}, n.failure("metadata", "decode", path, fmt.Errorf("parse recipe.json: %w", err))
	}
	// Retain the causal text should Core reject its empty-name observation; this
	// does not choose acceptance or return an error to Core on its behalf.
	if info.Name == "" {
		n.failure("metadata", "decode", path, fmt.Errorf("recipe.json has empty name in %s", n.bundleDir))
	}
	entry := ""
	if info.LibraryName != nil && *info.LibraryName != "" {
		entry = filepath.Join("..", "..", *info.LibraryName)
	}
	return process.ProjectRecipeMetadata{SourcePathB64: base64.StdEncoding.EncodeToString([]byte(path)), Name: info.Name, LibraryName: info.LibraryName, LibraryPathEntry: entry}, nil
}

func (n *nativeRecipeOwner) ObserveSkills(member string) (process.ProjectRecipeSkillsEvidence, *process.ProjectRecipeFailure) {
	// Dropping the previous owner is bounded even when Core chose no-op.
	n.root, n.skills, n.original, n.member = nil, nil, nil, ""
	absent := process.ProjectRecipeSkillsEvidence{Paths: []interface{}{}}
	path := filepath.Join(n.lingtaiDir, member, "init.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return absent, nil
		}
		return absent, n.failure("skills", "read", path, fmt.Errorf("read %s: %w", path, err))
	}
	var root map[string]interface{}
	if err := DecodeJSONUseNumber(data, &root); err != nil {
		return absent, n.failure("skills", "decode", path, fmt.Errorf("parse %s: %w", path, err))
	}
	stripObsoleteInitFields(root)
	manifest, ok := root["manifest"].(map[string]interface{})
	if !ok {
		return absent, nil
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		return absent, nil
	}
	changed, err := CanonicalizeCapabilities(caps)
	if err != nil {
		return absent, n.failure("skills", "normalize", path, fmt.Errorf("canonicalize %s: %w", path, err))
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		return absent, nil
	}
	original, _ := skills["paths"].([]interface{})
	paths := make([]interface{}, 0, len(original))
	for index, item := range original {
		if text, ok := item.(string); ok {
			paths = append(paths, text)
		} else {
			paths = append(paths, index)
		}
	}
	n.handle++
	n.root, n.skills, n.original, n.member = root, skills, original, member
	return process.ProjectRecipeSkillsEvidence{Present: true, CanonicalChanged: changed, Handle: n.handle, Paths: paths}, nil
}

func (n *nativeRecipeOwner) WriteSkills(member string, handle int, paths []interface{}) *process.ProjectRecipeFailure {
	path := filepath.Join(n.lingtaiDir, member, "init.json")
	if handle != n.handle || n.root == nil || n.skills == nil || n.member != member {
		return n.failure("skills", "write", path, fmt.Errorf("invalid Recipe document handle"))
	}
	root, skills, original := n.root, n.skills, n.original
	// Consume before writing: no replay after any partial write/rename failure.
	n.root, n.skills, n.original, n.member = nil, nil, nil, ""
	resolved := make([]interface{}, 0, len(paths))
	for _, entry := range paths {
		switch item := entry.(type) {
		case string:
			resolved = append(resolved, item)
		case int:
			if item < 0 || item >= len(original) {
				return n.failure("skills", "write", path, fmt.Errorf("invalid Recipe path reference"))
			}
			if _, text := original[item].(string); text {
				return n.failure("skills", "write", path, fmt.Errorf("invalid Recipe opaque entry"))
			}
			resolved = append(resolved, original[item])
		default:
			return n.failure("skills", "write", path, fmt.Errorf("invalid Recipe path command"))
		}
	}
	skills["paths"] = resolved
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return n.failure("skills", "write", path, fmt.Errorf("marshal %s: %w", path, err))
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return n.failure("skills", "write", path, fmt.Errorf("write %s: %w", tmp, err))
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return n.failure("skills", "write", path, fmt.Errorf("rename %s: %w", tmp, err))
	}
	return nil
}

func (n *nativeRecipeOwner) StatLayer(layer, language string) process.ProjectRecipeLayerStat {
	if n.bundleDir == "" {
		return process.ProjectRecipeLayerStat{}
	}
	root := filepath.Join(n.bundleDir, RecipeDotDir, layer)
	path := filepath.Join(root, layer+".md")
	if language != "" {
		path = filepath.Join(root, language, layer+".md")
	}
	info, err := os.Stat(path) // exactly once, at the original resolution point
	if err != nil {
		return process.ProjectRecipeLayerStat{}
	}
	return process.ProjectRecipeLayerStat{Exists: true, Directory: info.IsDir(), PathB64: base64.StdEncoding.EncodeToString([]byte(path))}
}

func (n *nativeRecipeOwner) ReadLayer(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	return data, err != nil
}

func resolveKernelRecipeLayer(bundleDir, lang, layer string) string {
	if bundleDir == "" {
		return ""
	}
	python, err := recipePython()
	if err != nil {
		return ""
	}
	selected, err := process.SelectProjectRecipeLayer(python, lang, layer, newNativeRecipeOwner(bundleDir, ""))
	if err != nil {
		return ""
	} // legacy path-only signature; never native fallback
	return selected
}

// ResolveRecipeSetupLayers is the save consumer's error-returning query. Legacy
// individual path wrappers stay source-compatible; unknown transport must stop
// the live save before configure rather than masquerade as an absent layer.
func ResolveRecipeSetupLayers(bundleDir, lang string) (string, string, error) {
	if bundleDir == "" {
		return "", "", nil
	}
	python, err := recipePython()
	if err != nil {
		return "", "", err
	}
	comment, err := process.SelectProjectRecipeLayer(python, lang, "comment", newNativeRecipeOwner(bundleDir, ""))
	if err != nil {
		return "", "", err
	}
	covenant, err := process.SelectProjectRecipeLayer(python, lang, "covenant", newNativeRecipeOwner(bundleDir, ""))
	return comment, covenant, err
}
