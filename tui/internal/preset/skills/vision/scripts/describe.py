"""Vision skill — describe an image with a locally-loaded Hugging Face VLM.

Usage:
    python describe.py <image-path> [--prompt PROMPT]
                                     [--model qwen2-vl-2b|moondream2|qwen2-vl-7b]
                                     [--model-id HF_REPO]
                                     [--device cpu|cuda|mps]
                                     [--max-new-tokens N]
                                     [--json-output]

Local-only. No API key, no network after weights are downloaded. First call
fetches model weights from Hugging Face Hub (2-15 GB depending on model);
subsequent calls reuse the cache.

For MiniMax-MCP-based image understanding, use the `understand_image` MCP
tool directly (see the `minimax-media` skill). This script exists for the
case where neither a vision-capable LLM nor a MiniMax key is available.

Emits a JSON document on stdout. Errors → stderr, exit non-zero.
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path


def _ensure(pkg: str, import_name: str | None = None) -> None:
    try:
        __import__(import_name or pkg.replace("-", "_"))
        return
    except ImportError:
        pass
    try:
        from lingtai.venv_resolve import ensure_package  # type: ignore
        ensure_package(pkg, import_name)
        return
    except Exception:
        pass
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", pkg])


_MODEL_ALIASES: dict[str, str] = {
    "moondream2": "vikhyatk/moondream2",
    "qwen2-vl-2b": "Qwen/Qwen2-VL-2B-Instruct",
    "qwen2-vl-7b": "Qwen/Qwen2-VL-7B-Instruct",
    "llava-1.6-mistral-7b": "llava-hf/llava-v1.6-mistral-7b-hf",
}


def _resolve_model_id(alias_or_id: str) -> str:
    if "/" in alias_or_id:
        return alias_or_id
    if alias_or_id in _MODEL_ALIASES:
        return _MODEL_ALIASES[alias_or_id]
    raise ValueError(
        f"Unknown model alias: {alias_or_id!r}. "
        f"Known aliases: {sorted(_MODEL_ALIASES)}, or pass --model-id <hf-repo>."
    )


# Module-level cache so batch loops keep the model resident in memory.
_LOCAL_CACHE: dict[tuple[str, str], tuple] = {}


def _pick_device(requested: str | None) -> str:
    if requested:
        return requested
    try:
        import torch
        if torch.cuda.is_available():
            return "cuda"
        if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
            return "mps"
    except Exception:
        pass
    return "cpu"


def _load(model_id: str, device: str) -> tuple:
    cache_key = (model_id, device)
    if cache_key in _LOCAL_CACHE:
        return _LOCAL_CACHE[cache_key]

    _ensure("transformers")
    _ensure("torch")
    _ensure("pillow", "PIL")

    from transformers import AutoModelForCausalLM, AutoProcessor

    if "moondream" in model_id.lower():
        _ensure("einops")
        from transformers import AutoTokenizer
        model = AutoModelForCausalLM.from_pretrained(
            model_id, trust_remote_code=True
        ).to(device)
        processor = AutoTokenizer.from_pretrained(model_id)
        kind = "moondream"
    elif "qwen2-vl" in model_id.lower():
        _ensure("qwen-vl-utils", "qwen_vl_utils")
        from transformers import Qwen2VLForConditionalGeneration
        model = Qwen2VLForConditionalGeneration.from_pretrained(
            model_id, torch_dtype="auto", device_map=device
        )
        processor = AutoProcessor.from_pretrained(model_id)
        kind = "qwen2-vl"
    elif "llava" in model_id.lower():
        from transformers import LlavaNextForConditionalGeneration
        model = LlavaNextForConditionalGeneration.from_pretrained(
            model_id, torch_dtype="auto", device_map=device
        )
        processor = AutoProcessor.from_pretrained(model_id)
        kind = "llava"
    else:
        model = AutoModelForCausalLM.from_pretrained(
            model_id, torch_dtype="auto", device_map=device, trust_remote_code=True
        )
        processor = AutoProcessor.from_pretrained(model_id, trust_remote_code=True)
        kind = "generic"

    _LOCAL_CACHE[cache_key] = (model, processor, kind)
    return _LOCAL_CACHE[cache_key]


def describe(
    image_path: str,
    *,
    prompt: str = "Describe this image.",
    model_alias: str = "qwen2-vl-2b",
    model_id: str | None = None,
    device: str | None = None,
    max_new_tokens: int = 512,
) -> str:
    """Run a local VLM on an image and return its text response.

    Importable for batch use:
        from describe import describe
        for img in images:
            print(describe(img, prompt="..."))
    The model is loaded once and cached at module scope.
    """
    if image_path.startswith(("http://", "https://")):
        raise ValueError(
            "Local backend does not accept URLs — download the image first "
            "(e.g. with curl) and pass the local path."
        )
    path = Path(image_path).expanduser().resolve()
    if not path.is_file():
        raise FileNotFoundError(f"image file not found: {path}")

    resolved_id = model_id or _resolve_model_id(model_alias)
    chosen_device = _pick_device(device)
    model, processor, kind = _load(resolved_id, chosen_device)

    from PIL import Image
    image = Image.open(path).convert("RGB")

    if kind == "moondream":
        enc = model.encode_image(image)
        return model.answer_question(enc, prompt, processor)

    if kind == "qwen2-vl":
        from qwen_vl_utils import process_vision_info  # type: ignore

        messages = [
            {
                "role": "user",
                "content": [
                    {"type": "image", "image": str(path)},
                    {"type": "text", "text": prompt},
                ],
            }
        ]
        text = processor.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
        image_inputs, video_inputs = process_vision_info(messages)
        inputs = processor(
            text=[text], images=image_inputs, videos=video_inputs,
            padding=True, return_tensors="pt",
        ).to(chosen_device)
        out_ids = model.generate(**inputs, max_new_tokens=max_new_tokens)
        trimmed = [o[len(i):] for i, o in zip(inputs.input_ids, out_ids)]
        return processor.batch_decode(trimmed, skip_special_tokens=True)[0].strip()

    if kind == "llava":
        conv = f"USER: <image>\n{prompt}\nASSISTANT:"
        inputs = processor(text=conv, images=image, return_tensors="pt").to(chosen_device)
        out_ids = model.generate(**inputs, max_new_tokens=max_new_tokens)
        full = processor.decode(out_ids[0], skip_special_tokens=True)
        if "ASSISTANT:" in full:
            return full.split("ASSISTANT:", 1)[1].strip()
        return full.strip()

    inputs = processor(text=prompt, images=image, return_tensors="pt").to(chosen_device)
    out_ids = model.generate(**inputs, max_new_tokens=max_new_tokens)
    return processor.decode(out_ids[0], skip_special_tokens=True).strip()


def _wrap_for_json(prompt: str) -> str:
    return (
        prompt
        + "\n\nRespond with a single JSON object. Do not wrap it in code fences. "
        "Do not add any text before or after the JSON."
    )


def main() -> int:
    p = argparse.ArgumentParser(
        description="Describe an image with a locally-loaded Hugging Face VLM.",
    )
    p.add_argument("image", help="Image path (local file).")
    p.add_argument("--prompt", default="Describe this image.")
    p.add_argument(
        "--model", default="qwen2-vl-2b",
        help="Model alias. Aliases: " + ", ".join(_MODEL_ALIASES) + ".",
    )
    p.add_argument("--model-id", default=None, help="Full HF repo id, overrides --model.")
    p.add_argument("--device", default=None, help="cpu / cuda / mps. Auto-detect if omitted.")
    p.add_argument("--max-new-tokens", type=int, default=512)
    p.add_argument("--json-output", action="store_true", help="Wrap prompt to request JSON output.")
    args = p.parse_args()

    prompt = _wrap_for_json(args.prompt) if args.json_output else args.prompt

    t0 = time.time()
    try:
        response = describe(
            args.image, prompt=prompt,
            model_alias=args.model, model_id=args.model_id,
            device=args.device, max_new_tokens=args.max_new_tokens,
        )
    except Exception as exc:
        print(json.dumps({"error": str(exc), "type": type(exc).__name__}), file=sys.stderr)
        return 1

    label_id = args.model_id or _MODEL_ALIASES.get(args.model, args.model)
    out = {
        "image": args.image,
        "backend": f"local-{label_id}",
        "prompt": args.prompt,
        "response": response,
        "elapsed_seconds": round(time.time() - t0, 2),
    }
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
