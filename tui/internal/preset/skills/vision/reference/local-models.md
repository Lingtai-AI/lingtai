# Local VLM Path — offline image understanding

Run a vision-language model on the user's machine. No API key, no per-call cost, no rate limits, full privacy. Pays for it in disk space (2-15 GB) and inference time (slow on CPU, fast on GPU/Apple Silicon).

## When to choose this path

- The user explicitly wants offline operation.
- You'll process many images (batch OCR, dataset labeling) and want predictable zero cost.
- Image content is sensitive (medical, internal docs, etc.) and shouldn't leave the machine.
- You've exhausted MiniMax quota.
- You're on a flight / boat / restricted network.

## Models — Pick One

| Model | Size on disk | RAM/VRAM | Speed (M2 Pro) | Quality |
|---|---|---|---|---|
| **moondream2** | ~1.6 GB | ~3 GB | ~2 s | Solid for short captions, captures, OCR snippets. Bad at long-form analysis. |
| **Qwen2-VL-2B-Instruct** | ~4 GB | ~6 GB | ~4 s | Best small VLM as of 2026. Good general-purpose. |
| **Qwen2-VL-7B-Instruct** | ~15 GB | ~16 GB | ~8 s | Near-Claude-3-Haiku quality. Needs serious hardware. |
| **LLaVA-1.6-Mistral-7B** | ~14 GB | ~16 GB | ~10 s | Older but well-documented. Use only if Qwen unavailable. |

For most users on most machines: **Qwen2-VL-2B**. Fast enough, good enough, fits in 8 GB RAM.

## Install

The bundled `scripts/describe.py --backend local` auto-installs dependencies via `lingtai.venv_resolve.ensure_package` (or pip fallback) on first run. You don't need to install anything manually — the first call will be slow (downloading model weights from Hugging Face Hub), subsequent calls reuse the cache.

If you want to pre-install:

```bash
# Common deps (all paths)
pip install transformers torch pillow

# Qwen2-VL specific
pip install qwen-vl-utils

# moondream specific
pip install einops
```

Models are cached under `~/.cache/huggingface/hub/`. Delete a directory there if you want to free space.

## Hardware Tips

- **Apple Silicon (M1/M2/M3)**: works out of the box via PyTorch MPS backend. The script auto-detects.
- **NVIDIA GPU**: ensure CUDA-enabled torch (`pip install torch --index-url https://download.pytorch.org/whl/cu124`). The script auto-uses CUDA if available.
- **CPU only**: works but slow. Stick to moondream2 unless you're patient.
- **AMD GPU on Linux**: ROCm version of torch needed; experimental.

## Calling the Local Backend

```bash
# Default model (qwen2-vl-2b)
python scripts/describe.py image.png --backend local

# Specific model
python scripts/describe.py image.png --backend local --model moondream2
python scripts/describe.py image.png --backend local --model qwen2-vl-7b

# Force CPU (useful for debugging or if GPU is busy)
python scripts/describe.py image.png --backend local --model qwen2-vl-2b --device cpu
```

Available `--model` values:
- `moondream2`
- `qwen2-vl-2b` (default)
- `qwen2-vl-7b`
- `llava-1.6-mistral-7b`

For other models from Hugging Face, pass the full repo ID with `--model-id`:
```bash
python scripts/describe.py image.png --backend local --model-id "<org>/<model>"
```
The script will try generic Transformers loading; provider-specific quirks may not work.

## Prompt Templates Per Model

Different VLMs respond best to different prompt shapes. The script handles this automatically via per-model templates, but if you're calling the model directly:

**moondream2** — terse, single-question:
```
"Describe this image."
"Read the text in this image."
"Is there a person in this image? Answer yes or no."
```

**Qwen2-VL** — handles long, structured prompts well:
```
"Analyze this chart. Output JSON with the following schema: {chart_type, x_axis_label, y_axis_label, data_points: [{label, value}]}. Only output valid JSON, no commentary."
```

**LLaVA** — needs explicit role framing:
```
"USER: <image> Describe this image in detail.\nASSISTANT:"
```

## Batch Patterns

Local models shine for batch work because there's no per-call latency from API round-trips (just inference time). Pattern:

```bash
# Pre-warm the model (loads weights once)
python scripts/describe.py warmup.png --backend local --model qwen2-vl-2b > /dev/null

# Then loop — model stays in memory across calls if you keep a single process
for f in images/*.jpg; do
  python scripts/describe.py "$f" --backend local --model qwen2-vl-2b \
    --prompt "Read all visible text. Output only the text." \
    | jq -r .response > "ocr/$(basename "$f" .jpg).txt"
done
```

For *true* batching (one model load, many images in one call), write a Python script that imports the script's `analyze_local` function and calls it in a loop. See [batch-patterns.md](batch-patterns.md) for that approach.

## Failure Modes

| Symptom | Cause | Fix |
|---|---|---|
| `OSError: ... is not a local folder ...` | Model name typo | Use one of the known-model aliases or pass `--model-id <hf-repo>` |
| `torch.cuda.OutOfMemoryError` | Model too big for GPU | Use a smaller model or `--device cpu` |
| `Killed` (process disappears) | OS OOM-killed for using too much RAM | Smaller model |
| Hangs on first run | Downloading weights from HF | Wait — 4 GB at typical home connection ≈ 10 min |
| `gated repo` error | HF login needed for the model | `huggingface-cli login` with a token |
| Output is gibberish | Wrong prompt template for model | Check the prompt-template section above |
| Response in wrong language | Model defaulted to training-set bias | Add explicit language directive in prompt |

## Privacy Note

Local inference means images never leave the machine. But the model weights came from Hugging Face — that download is logged on their side. If you need *zero* network traffic after install, set the env var `TRANSFORMERS_OFFLINE=1` for subsequent runs, and ensure you're not using the script with `--backend mcp` accidentally.
