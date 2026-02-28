# Vision Example — Multimodal (Image) Support

## Why

Users need to ask questions about images and screenshots (e.g. "What error is this?", "Describe this diagram", "What does this sign say?"). Genie supports vision-capable models by passing image attachments as visual content to the LLM.

## Problem

Without multimodal support, the assistant could only see a text description of attachments, not the actual pixels. That limits use cases like screenshot debugging, document analysis, and visual Q&A.

## Benefit

With this example you can run Genie configured for vision: the user sends a message with an image attachment (when the platform provides a local path), and the model receives the image and can answer from the visual content.

## Data flow (where it goes)

1. **User message + attachment** (with `LocalPath`) → messenger or chat UI → orchestrator.
2. **Orchestrator** → reactree → expert with `Attachments` set.
3. **Expert** `buildUserMessage` builds a `model.Message`: text plus `AddImageFilePath(att.LocalPath, "auto")` for image attachments (and `AddFilePath` for video/other files).
4. That **model.Message** is passed to the trpc-agent-go runner → LLM (e.g. Gemini) → response back to the user.
5. **No trpc-agent-go storage** is used for the attachment bytes; only the path is read and sent to the model.

## Arrange

1. Set `GEMINI_API_KEY` (or another vision provider’s key) in the environment.
2. From the repo root, run Genie with this example config:
   ```bash
   GENIE_CONFIG=examples/vision/.genie.toml genie grant
   ```
   Or copy `examples/vision/.genie.toml` to your project root as `.genie.toml` and run `genie grant`.
3. Have an image file available (e.g. a screenshot or photo) and a platform that sets `LocalPath` on attachments (e.g. WhatsApp after media download), or use the chat UI once attachment upload is supported.

## Act

1. Connect to the running Genie instance (e.g. open `docs/chat.html` and point to the AG-UI server, or use WhatsApp if configured).
2. Send a message that includes an image attachment (e.g. "What's in this screenshot?" with an image). Ensure the attachment has a local path so the model receives the image.
3. If testing without a platform that sets `LocalPath`, you can use a small script or future API that sends a question and an attachment path; the backend will call `buildUserMessage` with that attachment and the model will receive it.

## Assert

- The model’s reply should reference the **content of the image** (e.g. description of what’s visible, extracted text, or an answer to the question).
- If the model responds with a generic "I don’t see an image" or ignores the image, check that the attachment had `LocalPath` set and that the configured provider supports vision (e.g. Gemini, GPT-4V).
