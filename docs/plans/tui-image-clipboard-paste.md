# TUI clipboard image paste spike

Issue: <https://github.com/Lingtai-AI/lingtai/issues/466>

## Goal

Let a human paste a screenshot/image into the LingTai TUI compose box without first saving it manually. The first usable shape is a local-terminal MVP: press `Ctrl+V`, read image bytes from the system clipboard, save them under the project `.lingtai/human` area, and insert an absolute file-path marker into the outgoing message so the agent can call `vision` on it.

## What other terminal tools appear to do

- Claude Code-style flows expose a dedicated image paste shortcut (`Ctrl+V` on macOS/Linux in user-facing docs; some Windows flows use `Alt+V`) rather than treating terminal text paste as image bytes.
- GitHub Copilot CLI discussions describe Windows Terminal emitting an empty bracketed paste sequence for image-only clipboards, then the CLI falls back to reading image data from a clipboard manager.
- VS Code terminal/image-paste extensions commonly save the clipboard image to a project file and insert the path/markdown reference.
- Shell-out paths are fragmented: `pbpaste` is text-oriented, X11 uses `xclip`, Wayland uses `wl-paste`, Windows needs Win32/PowerShell, and SSH only sees the remote clipboard unless a client-side relay exists.

## Current LingTai TUI constraints

- Text paste already works through Bubble Tea v2 `tea.PasteMsg` forwarded to the focused `textarea`; this must remain untouched.
- The compose path currently sends text only: `MailModel` ultimately calls `fs.WriteMail(..., text)`. Attachment fields exist for display/history but the composer does not populate them.
- The TUI does not have a reliable, single capability gate for whether the selected model/agent can use vision.
- Remote/SSH clipboard access is out of scope for the MVP because the clipboard is on the client, not the remote host.

## MVP contract

1. Handle `Ctrl+V` as a dedicated image-paste shortcut in the mail view.
2. Do **not** intercept `tea.PasteMsg`; ordinary text paste remains the textarea's job.
3. Read image bytes through `golang.design/x/clipboard` (`FmtImage`) to avoid bespoke per-platform shell scripts.
4. Save the image under `<project>/.lingtai/human/attachments/pasted-images/` with mode `0600`.
5. Append `[pasted image: <absolute-path>]` to the compose box on its own line; the human can edit or delete it before sending.
6. Show a short status flash for success/failure.

## Known limits / follow-ups

- The dependency brings transitive clipboard/X11/image packages; packaging impact should be reviewed before PR merge.
- The MVP inserts a path marker rather than extending the mail schema with true outgoing attachments. A future attachment contract could make this cleaner.
- Vision capability gating is not enforced yet. The path marker is transparent; unsupported agents can ask for another input, and supported agents can call `vision`.
- SSH/client-relay support is not attempted.
- Footer shortcut copy does not advertise `Ctrl+V` yet to avoid crowding the status bar; discoverability can be solved after the interaction is accepted.
