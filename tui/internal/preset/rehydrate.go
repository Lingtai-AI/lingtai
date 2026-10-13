package preset

import "fmt"

// RehydrateNetwork is a retired split-operation entry point. It cannot safely
// infer the selected source configure request after init.json already exists.
// Production imports use process.RehydrateProject with the public request,
// BEFORE source initialization. Never recover a mixed network with a fallback.
func RehydrateNetwork(lingtaiDir, orchDirName string) (int, error) {
	return 0, fmt.Errorf("split rehydration is unsupported; configure source and imported workers together through kernel project rehydrate before any source init write")
}
