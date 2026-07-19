# Experimental Windows amd64 Preview

LingTai publishes an experimental Windows amd64 preview containing only:

```text
lingtai-tui.exe
lingtai-portal.exe
WINDOWS-PREVIEW.md
```

The release also carries the zip's `.sha256` sidecar and
`install-windows-preview.ps1`.

This is **not** a supported native Windows runtime. It does not install or
validate the Python kernel, agent lifecycle interoperability, terminal/CJK and
SQLite behavior, PATH or services, code signing, arm64, or the built-in updater.
macOS, Linux, and WSL remain the supported install paths.

## Install or update the preview

Download and inspect `install-windows-preview.ps1` from the same GitHub release,
then run it as an ordinary user with an explicit tag:

```powershell
.\install-windows-preview.ps1 -Version v0.X.Y
```

For a local or Actions artifact, provide both verified files:

```powershell
.\install-windows-preview.ps1 `
  -ArchivePath .\lingtai-v0.X.Y-windows-amd64-preview.zip `
  -ChecksumPath .\lingtai-v0.X.Y-windows-amd64-preview.zip.sha256
```

Rerun the script to update. It refuses to replace running preview binaries,
checks SHA256 and the exact three archive members, stages the replacement, and
restores the previous directory if replacement fails. It does not edit PATH or
write runtime/updater metadata.

After install, smoke-test the two binaries:

```powershell
& "$env:LOCALAPPDATA\LingTai\preview\lingtai-tui.exe" version
& "$env:LOCALAPPDATA\LingTai\preview\lingtai-portal.exe" version
```

To uninstall, stop both binaries and remove
`$env:LOCALAPPDATA\LingTai\preview`.
