# StopProcess · Contract

Command: deliver at most one native stop request per call to a process whose
identity is verified immediately before delivery.

- Authority covers only the delivery event. Delivery is NOT exit proof; no
  waiting, polling, escalation, or exit observation happens here.
- Inputs: Target{PID > 0, expected executable base name, no `.exe`} plus
  Kind (Graceful | Forceful).
- Results, distinct and never merged: delivered · stale-identity ·
  access-denied · already-absent · unsupported · unknown. `unknown` carries
  an error and must never be read as delivered or absent. Only `delivered`
  signals the target; every other outcome leaves it untouched.
- POSIX (adapter_linux.go / adapter_darwin.go): identity is the kernel
  command name (`/proc/<pid>/comm`, `kern.proc.pid` sysctl) compared under
  kernel truncation (15/16 bytes); delivery is kill(SIGTERM|SIGKILL). The
  verify→signal window is two syscalls — documented, not hidden.
- Windows (adapter_windows.go): Graceful is honestly `unsupported`. Forceful
  verifies image name and liveness, then calls TerminateProcess on the SAME
  held handle, so the PID cannot be recycled in between.
- Forbidden inferences: delivered ⇒ exited; already-absent ⇒ never existed;
  unknown/unsupported ⇒ stopped. Never observes termination, scans process
  lists, or consults any other interface.
