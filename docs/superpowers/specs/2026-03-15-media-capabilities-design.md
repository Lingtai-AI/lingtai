# Media Capabilities & Mail Attachments Design

**Date:** 2026-03-15
**Status:** Draft

## Overview

Add four new media capabilities (draw, compose, talk, listen) and filesystem-based mail attachments to StoAI. Media capabilities follow the capability pattern (opt-in via `add_capability()`), routing through LLMService/LLMAdapter methods. Mail attachments extend the existing mail intrinsic with inline file transfer and a filesystem-based mailbox.

## Part 1: Media Capabilities

### Four New Capabilities

| Capability | Tool name | What it does | Output |
|-----------|-----------|-------------|--------|
| draw | `draw` | Text-to-image generation | Image file path |
| compose | `compose` | Music generation from prompt | Audio file path |
| talk | `talk` | Text-to-speech | Audio file path |
| listen | `listen` | Speech transcription + audio analysis | Text |

### Capability Modules

Four new files in `capabilities/`:

- `capabilities/draw.py` — `setup(agent, **kwargs)`
- `capabilities/compose.py` — `setup(agent, **kwargs)`
- `capabilities/talk.py` — `setup(agent, **kwargs)`
- `capabilities/listen.py` — `setup(agent, **kwargs)`

Each `setup()` registers a tool via `agent.add_tool()` and injects a system prompt section via `agent.update_system_prompt()`.

**Registration in `capabilities/__init__.py`:**

```python
_BUILTIN = {
    "bash": ...,
    "delegate": ...,
    "email": ...,
    "draw": "stoai.capabilities.draw",
    "compose": "stoai.capabilities.compose",
    "talk": "stoai.capabilities.talk",
    "listen": "stoai.capabilities.listen",
}
```

**Usage:**

```python
agent.add_capability("draw")
agent.add_capability("draw", "compose", "talk", "listen")  # multiple at once
```

### Tool Schemas

**draw:**
```json
{
  "type": "object",
  "properties": {
    "prompt": { "type": "string", "description": "Description of the image to generate" }
  },
  "required": ["prompt"]
}
```

**compose:**
```json
{
  "type": "object",
  "properties": {
    "prompt": { "type": "string", "description": "Description of the music to generate" },
    "duration_seconds": { "type": "number", "description": "Desired duration in seconds" }
  },
  "required": ["prompt"]
}
```

**talk:**
```json
{
  "type": "object",
  "properties": {
    "text": { "type": "string", "description": "Text to convert to speech" }
  },
  "required": ["text"]
}
```

**listen:**
```json
{
  "type": "object",
  "properties": {
    "audio_path": { "type": "string", "description": "Path to the audio file" },
    "mode": { "type": "string", "enum": ["transcribe", "analyze"], "description": "Transcribe speech or analyze audio content", "default": "transcribe" },
    "prompt": { "type": "string", "description": "Question about the audio (for analyze mode)" }
  },
  "required": ["audio_path"]
}
```

### Handler Pattern

No fallback chain. One path — call the LLM adapter method, succeed or fail:

1. Call `agent.service.<method>()` (routes to configured provider)
2. Success → save file to `media/` subfolder, return `{ status: "ok", file_path: "..." }`
3. Failure or provider not configured → return `{ status: "error", message: "..." }`

### Media File Output Structure

Generated files are saved under the agent's working directory:

```
<working_dir>/
  media/
    images/       <- draw output
    music/        <- compose output
    audio/        <- talk (TTS) output
```

**File naming:** `{tool}_{timestamp}_{short_hash}.{ext}` (e.g., `draw_20260315_a3f2.png`)

Directories are auto-created on first use.

### LLM Layer Extensions

**LLMAdapter (base.py)** — new optional methods, default raises NotImplementedError or returns empty:

```python
def generate_image(self, prompt: str, model: str) -> bytes:
    """Text-to-image. Returns image bytes (PNG)."""
    return b""

def generate_music(self, prompt: str, model: str, duration_seconds: float | None = None) -> bytes:
    """Text-to-music. Returns audio bytes."""
    return b""

def text_to_speech(self, text: str, model: str) -> bytes:
    """TTS. Returns audio bytes."""
    return b""

def transcribe(self, audio_bytes: bytes, model: str) -> str:
    """Speech-to-text. Returns transcription."""
    return ""

def analyze_audio(self, audio_bytes: bytes, prompt: str, model: str) -> str:
    """Audio analysis. Returns text description."""
    return ""
```

**LLMService (service.py)** — new gateway methods, same pattern as `generate_vision()` / `web_search()`:

```python
def generate_image(self, prompt: str) -> bytes:
    """Route to configured image_provider."""

def generate_music(self, prompt: str, duration_seconds: float | None = None) -> bytes:
    """Route to configured music_provider."""

def text_to_speech(self, text: str) -> bytes:
    """Route to configured tts_provider."""

def transcribe(self, audio_bytes: bytes) -> str:
    """Route to configured audio_provider."""

def analyze_audio(self, audio_bytes: bytes, prompt: str) -> str:
    """Route to configured audio_provider."""
```

Each resolves a provider name from config (e.g., `image_provider`, `music_provider`, `tts_provider`, `audio_provider`), gets the adapter, and calls the method. Returns empty on missing provider — the capability handler treats empty as failure.

## Part 2: Mail Attachments

### Message Model

Add `attachments` field to the mail message:

```python
@dataclass
class MailMessage:
    sender: str
    recipient: str
    body: str
    attachments: list[str] = field(default_factory=list)  # file paths on sender side
```

### Filesystem-Based Mailbox

Each received message is persisted as a folder:

```
<working_dir>/
  mailbox/
    msg_001/
      message.json         <- sender, body, timestamp, metadata
      attachments/
        draw_abc.png       <- actual file bytes, decoded and saved
    msg_002/
      message.json
      attachments/
        song.mp3
        image.png
```

Message numbering is sequential per agent.

### Wire Protocol (TCP Transport)

Attachments are transferred inline via base64 encoding:

1. **Sender side:** MailService reads attachment files from sender's filesystem, base64-encodes the bytes, includes them in the serialized message alongside filenames.
2. **Wire format:** Extended message JSON includes an `attachments` array of `{ filename: str, data: str (base64) }`.
3. **Receiver side:** MailService decodes, creates `mailbox/msg_XXX/attachments/` directory, saves files there. The delivered `MailMessage.attachments` contains the local paths (e.g., `mailbox/msg_001/attachments/draw_abc.png`).

### Attachment Usage Convention

Files always remain in the mailbox. When an agent wants to use an attachment elsewhere:

- Create a **symlink** at the desired location pointing to the mailbox copy
- Example: `media/images/received_portrait.png -> mailbox/msg_001/attachments/portrait.png`
- The mailbox is the source of truth; symlinks are references

This convention is communicated to the agent via the mail system prompt section.

### Mail Intrinsic Schema Update

Add optional `attachments` parameter:

```json
{
  "attachments": {
    "type": "array",
    "items": { "type": "string" },
    "description": "List of file paths to attach to the message"
  }
}
```

### Email Capability Updates

The email capability inherits attachment support from mail. Updates:

- `send_email` tool gains `attachments` parameter
- `read_email` / inbox display shows attachment file paths
- `forward` carries attachments
- `reply` / `reply_all` can optionally include attachments

## No New Service ABCs

Unlike vision/search which have dedicated service ABCs, the four media capabilities route entirely through the LLM layer (LLMService -> LLMAdapter). No `DrawService`, `ComposeService`, `TTSService`, or `ListenService` ABCs. Provider implementations are LLM adapter methods.

## Files to Create/Modify

### New files:
- `src/stoai/capabilities/draw.py`
- `src/stoai/capabilities/compose.py`
- `src/stoai/capabilities/talk.py`
- `src/stoai/capabilities/listen.py`

### Modified files:
- `src/stoai/capabilities/__init__.py` — register 4 new capabilities in `_BUILTIN`
- `src/stoai/llm/base.py` — add 5 new LLMAdapter methods
- `src/stoai/llm/service.py` — add 5 new LLMService gateway methods + 4 provider config keys
- `src/stoai/services/mail.py` — add `attachments` to MailMessage, update MailService ABC, update TCPMailService wire protocol, add filesystem-based mailbox persistence
- `src/stoai/intrinsics/mail.py` — add `attachments` to schema
- `src/stoai/capabilities/email.py` — support attachments in send/read/forward/reply
- `src/stoai/prompt.py` or agent system prompt sections — attachment symlink convention instructions
