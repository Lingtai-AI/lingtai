package preset

import (
	"os"

	"github.com/anthropics/lingtai-tui/internal/atomicfile"
)

// atomicWriteFile writes data to path via the shared temp-file-plus-rename
// primitive so a crash or power loss mid-write cannot leave the target
// truncated or empty.
//
// Critical config files (init.json, .agent.json) MUST be written through this
// helper: a partial write to one of them leaves the agent unlaunchable. The
// recipe-apply and recipe-state paths already inline this same pattern; this is
// the shared primitive for the config writes that had not been backfilled.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return atomicfile.Write(path, data, perm)
}
