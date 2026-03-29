# Known Limitations

## Phantom Processes

**What**: Deleting an agent's working directory while its process is still running creates an orphaned Python process. The process continues running in memory with no way to reach it via signal files (since the directory is gone).

**Why this happens**: Agents are managed via filesystem signal files (`.suspend`, `.sleep`, `.interrupt`). The agent's heartbeat thread polls these files every second. If the directory is deleted, the signal files have nowhere to be written, so the process can't receive shutdown commands.

**Why we don't fix this**: A PID-based kill mechanism was considered and intentionally rejected. `SIGTERM` is Unix-only (`syscall.SIGTERM` doesn't exist on Windows), and adding platform-specific process management for a case that only occurs through manual directory deletion adds complexity without proportional benefit. The graceful shutdown flow handles all normal cases.

**How to avoid it**: Always suspend agents before removing their directories.

```bash
# In the TUI:
/suspend        # suspend the current agent
/suspend-all    # suspend all agents in the project

# Then safely remove:
rm -rf .lingtai/my-agent/
```

**How to clean up phantom processes**:

```bash
# Find orphaned lingtai processes
ps aux | grep "lingtai run"

# Kill them
kill <pid>
```

## Heavy Optional Dependencies

The `listen` capability depends on `faster-whisper` (~132 MB) and `librosa` (~202 MB). These are **not** installed with `pip install lingtai`. They are automatically installed on first use when an agent actually invokes the listen tool (transcription or music analysis). The first invocation will pause for a few seconds while the packages download.
