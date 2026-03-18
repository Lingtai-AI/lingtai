DEFAULTS = {
    "api_compat": "openai",
    "base_url": "https://api.moonshot.ai/v1",
    # Moonshot uses the same endpoint for both OpenAI and Anthropic compat.
    # Override in config.json if they add a separate Anthropic-compatible URL.
    "base_url_anthropic": "https://api.moonshot.ai/v1",
    "api_key_env": "KIMI_API_KEY",
    "model": "kimi-k2.5",
}
