# Embedding Providers (HuggingFace & Gemini) — Acceptance Criteria

> Tests for HuggingFace TEI and Gemini embedding provider configuration, auto-detection, and error handling.

---

## 20 — Embedding: Gemini Provider Config

### Why
Validates that the Gemini embedding provider can be configured and rejects startup when the API key is missing.

### Problem
A misconfigured Gemini provider could silently fall back to the dummy embedder, giving users the false impression that memory is semantic when it is not.

### Benefit
Ensures explicit, early failure when the Gemini provider is requested but credentials are absent.

### Arrange
- Create a `.genie.toml` with:
```toml
[vector_memory]
embedding_provider = "gemini"
gemini_api_key = ""
```

### Act
```bash
build/genie grant
```

### Assert
- Server fails to start with an error containing `gemini provider requested but no API key found`
- The error message mentions `GOOGLE_API_KEY`

---

## 21 — Embedding: Gemini Auto-Detection via Env Var

### Why
When `GOOGLE_API_KEY` is set and no explicit provider is chosen, Genie should auto-select the Gemini embedding provider.

### Problem
Without auto-detection, users who set `GOOGLE_API_KEY` but forget to set `embedding_provider` would silently get the dummy embedder.

### Benefit
Reduces configuration friction — setting an API key is enough.

### Arrange
- No `.genie.toml` file
- Set env: `export GOOGLE_API_KEY=test-key`

### Act
```bash
build/genie grant 2>&1 | head -20
```

### Assert
- Startup logs should show the embedding provider as `gemini` (or fail with a Gemini-specific error, confirming auto-detection)
- Should NOT show `dummy` as the embedding provider

---

## 22 — Embedding: HuggingFace TEI Provider Config

### Why
Validates that the HuggingFace TEI embedding provider defaults correctly and connects to the expected URL.

### Problem
If the default URL is wrong or the provider fails to initialize, vector memory will silently degrade.

### Benefit
Ensures HuggingFace TEI works with default configuration when a TEI server is running locally.

### Arrange
- Create a `.genie.toml` with:
```toml
[vector_memory]
embedding_provider = "huggingface"
```
- Optionally run a TEI server: `docker run -p 8080:80 ghcr.io/huggingface/text-embeddings-inference:latest`

### Act
```bash
build/genie grant
```

### Assert
- If TEI is running: server starts successfully and embeddings work
- If TEI is NOT running: server starts but memory operations fail with a connection error to `localhost:8080`
- No panic or crash occurs either way

---

## 23 — Embedding: HuggingFace Auto-Detection via Env Var

### Why
When `HUGGINGFACE_URL` is set and no provider or higher-priority key is configured, Genie should auto-select `huggingface`.

### Problem
Without auto-detection, users who set `HUGGINGFACE_URL` would still get the dummy embedder.

### Benefit
Convention-over-configuration — setting the URL is enough.

### Arrange
- No `.genie.toml` file
- Unset `OPENAI_API_KEY` and `GOOGLE_API_KEY`
- Set env: `export HUGGINGFACE_URL=http://localhost:8080`

### Act
```bash
build/genie grant 2>&1 | head -20
```

### Assert
- Startup shows `huggingface` as the embedding provider
- Should NOT show `dummy` or `openai`

---

## 24 — Embedding: Provider Priority Chain

### Why
When multiple env vars are set, the auto-detection must follow the documented priority: openai > gemini > huggingface.

### Problem
Ambiguous priority could cause unexpected provider selection when multiple API keys are present.

### Benefit
Deterministic, documented behavior when multiple credentials coexist.

### Arrange
- Set env:
```bash
export OPENAI_API_KEY=sk-test
export GOOGLE_API_KEY=test-key
export HUGGINGFACE_URL=http://localhost:8080
```

### Act
```bash
build/genie grant 2>&1 | head -20
```

### Assert
- Embedding provider should be `openai` (highest priority)
- Unsetting `OPENAI_API_KEY` should fall through to `gemini`
- Unsetting both `OPENAI_API_KEY` and `GOOGLE_API_KEY` should fall through to `huggingface`
