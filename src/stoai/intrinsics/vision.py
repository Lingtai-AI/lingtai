"""Vision intrinsic — image understanding via LLM."""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "image_path": {"type": "string", "description": "Path to the image file"},
        "question": {"type": "string", "description": "Question about the image", "default": "Describe this image."},
    },
    "required": ["image_path"],
}
DESCRIPTION = (
    "Analyze an image using the LLM's vision capabilities. "
    "Supports JPEG, PNG, and WebP. Ask any question about the image — "
    "describe contents, read text, interpret charts, identify objects, "
    "assess style or mood. Combine with draw to generate then analyze images."
)
