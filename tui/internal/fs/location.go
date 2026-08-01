// internal/fs/location.go
package fs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ipinfoResponse is the JSON shape returned by http://ipinfo.io/json.
type ipinfoResponse struct {
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
	Loc      string `json:"loc"`
}

var humanLocationManifestLocks sync.Map // canonical manifest path -> *sync.Mutex

func humanLocationManifestMutex(path string) *sync.Mutex {
	key, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		key = filepath.Clean(path)
	}
	lock, _ := humanLocationManifestLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// RemoveHumanManifestForReset crosses the same canonical manifest mutex as
// UpdateHumanLocation, then removes that writer's commit target. Call it at the
// final reset boundary immediately before recursively deleting the project tree;
// never hold this mutex across discovery, agent suspension, or RemoveAll.
func RemoveHumanManifestForReset(humanDir string) error {
	manifestPath := filepath.Join(humanDir, ".agent.json")
	mutex := humanLocationManifestMutex(manifestPath)
	mutex.Lock()
	defer mutex.Unlock()

	err := os.Remove(manifestPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ResolveLocation queries ipinfo.io and returns a populated Location.
func ResolveLocation() (Location, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://ipinfo.io/json")
	if err != nil {
		return Location{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Location{}, fmt.Errorf("ipinfo.io returned %d", resp.StatusCode)
	}

	var info ipinfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return Location{}, err
	}

	return Location{
		City:       info.City,
		Region:     info.Region,
		Country:    info.Country,
		Timezone:   info.Timezone,
		Loc:        info.Loc,
		ResolvedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// LocationStale reports whether loc needs to be refreshed.
// It returns true if ResolvedAt is empty, unparseable, or older than maxAge.
func LocationStale(loc Location, maxAge time.Duration) bool {
	if loc.ResolvedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, loc.ResolvedAt)
	if err != nil {
		return true
	}
	return time.Since(t) > maxAge
}

// UpdateHumanLocation resolves and stores a stale human location. Same-manifest
// callers serialize across the stale check, network request, and commit so a
// waiter observes the first caller's fresh result instead of resolving again.
// The commit rereads the latest valid manifest and changes only location.
// The operation remains best-effort and is a no-op on any failure.
func UpdateHumanLocation(humanDir string) {
	manifestPath := filepath.Join(humanDir, ".agent.json")
	mutex := humanLocationManifestMutex(manifestPath)
	mutex.Lock()
	defer mutex.Unlock()

	raw, err := ReadAgentRaw(humanDir)
	if err != nil || raw == nil {
		return
	}
	if !LocationStale(locationFromManifest(raw), time.Hour) {
		return
	}

	resolved, err := ResolveLocation()
	if err != nil {
		return
	}
	storeResolvedHumanLocationLocked(humanDir, manifestPath, resolved)
}

// StoreResolvedHumanLocation synchronously merges an already-resolved location
// into the latest valid human manifest. It shares UpdateHumanLocation's
// canonical-path mutex, allowing callers such as recipe rendering to reuse a
// lookup without starting a second resolver or writer.
func StoreResolvedHumanLocation(humanDir string, resolved Location) {
	manifestPath := filepath.Join(humanDir, ".agent.json")
	mutex := humanLocationManifestMutex(manifestPath)
	mutex.Lock()
	defer mutex.Unlock()
	storeResolvedHumanLocationLocked(humanDir, manifestPath, resolved)
}

func storeResolvedHumanLocationLocked(humanDir, manifestPath string, resolved Location) {
	latest, err := ReadAgentRaw(humanDir)
	if err != nil || latest == nil {
		return
	}
	if !LocationStale(locationFromManifest(latest), time.Hour) {
		return
	}

	latest["location"] = resolved
	data, err := json.MarshalIndent(latest, "", "  ")
	if err != nil {
		return
	}
	_ = writeAtomicBytes(manifestPath, data, 0o644)
}

func locationFromManifest(raw map[string]interface{}) Location {
	var location Location
	encoded, err := json.Marshal(raw["location"])
	if err != nil {
		return location
	}
	_ = json.Unmarshal(encoded, &location)
	return location
}
