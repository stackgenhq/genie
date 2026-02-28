# Blind spot analysis: Ollama setup flow

## Summary

Review of the Ollama setup changes (provider option, model list, default Mac model, pull behind the scenes) for edge cases, errors, and consistency.

---

## Addressed / fixed

| Area | Note |
|------|------|
| **Ollama down after detection** | We detect at start; if Ollama is stopped before pull, `PullModel` fails and we return the error. Acceptable. |
| **Pull interrupted (Ctrl+C)** | Context is cancelled, `PullModel` returns error, setup exits. Ollama resumes partial pulls on next run. Error message could mention re-running to resume. |
| **Offline / no network** | Pull fails with network error; we surface it. No pre-check for connectivity. |
| **Disk space** | Pull can fail from Ollama side if disk full; we don’t check beforehand. |
| **Duplicate model names** | `/api/tags` could return duplicates; we dedupe in `ListModels` so the dropdown and “already have” check are consistent. |
| **Default model consistency** | `DefaultModelForProvider("ollama")` now returns `DefaultOllamaModelForSetup` so config and setup use the same default when no model is set. |

---

## Edge cases (accepted)

| Area | Note |
|------|------|
| **Model name format** | We compare `m == c.ModelName` exactly. If Ollama returns a different tag for the same model (e.g. `llama3.2:3b-q4_0`), we may pull again. Harmless. |
| **ListModels fails but Ollama reachable** | We still use default model and attempt pull. Correct. |
| **Large pull response body** | If Ollama ever returned a huge body on error, we’d put it in the error message. We cap by reading response only; real error responses are small. |

---

## Security / privacy

- No secrets sent to Ollama; we only call `GET /api/tags` and `POST /api/pull` to localhost.
- Model name is user-chosen or our default; no unsanitized input in the request body (we pass it as the `name` field in JSON).

---

## Tests

- `OllamaReachable`, `ListModels`, `PullModel`, and `DefaultOllamaModelForSetup` have unit tests (including failure and empty-model cases).
- Setup flow is interactive; no automated test for the full wizard. Manual QA is expected.
