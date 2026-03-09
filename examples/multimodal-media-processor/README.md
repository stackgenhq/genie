# Multimodal Media Processor — Customer Support Intelligence

## Why

Customers across industries communicate with support teams using **more than text** — they send photos of damaged products, voice notes describing issues, videos of malfunctions, and scans of receipts. An AI support agent that can only read text misses the majority of the signal. This example demonstrates how Genie processes all media types to deliver intelligent, context-aware support.

## Problem

Without multimodal support, a support agent would:
- Ask customers to _describe_ what they see in words (frustrating UX)
- Miss visual evidence of damage severity
- Fail to process voice messages (very common on WhatsApp)
- Require manual triage of photos and videos
- Lose context from documents that customers attach

## Benefit

With multimodal capabilities, the agent can:
- **See** product damage and classify severity automatically
- **Hear** voice messages and respond in the customer's language
- **Watch** videos of product malfunctions and suggest fixes
- **Read** receipts, invoices, and documents to extract order info
- **Triage** issues with priority labels based on visual/audio evidence

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ Customer (WhatsApp / AG-UI)                                     │
│  📷 Image  🎤 Audio  🎥 Video  📄 Document  💬 Text            │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                    ┌──────▼──────┐
                    │  Messenger  │  (WhatsApp adapter / AG-UI)
                    │  Adapter    │  Downloads media, creates
                    │             │  Attachment{LocalPath, MIME}
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ Orchestrator│  Passes Attachments to Expert
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   Expert    │  buildUserMessage():
                    │             │  • image/* → AddImageData()   ✅ full MIME
                    │             │  • audio/* → AddAudioFilePath()  ✅ with OGG→WAV
                    │             │  • video/* → AddFileData()    ✅ explicit MIME
                    │             │  • other   → AddFilePath()
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Gemini 2.0 │  Natively processes:
                    │    Flash    │  • Images (vision)
                    │             │  • Audio (understanding)
                    │             │  • Video (via File API)
                    │             │  • Documents (PDF, etc.)
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   Tools     │
                    │ • OCR       │  For text extraction from images
                    │ • Shell     │  For ffmpeg media conversion
                    │ • WebFetch  │  For KB article lookup
                    │ • YouTube   │  For tutorial transcripts
                    └─────────────┘
```

## Data Flow — Where Each Media Type Goes

### Images (✅ Fully Working)
1. **WhatsApp**: `GetImageMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "image/jpeg"}`
2. **AG-UI**: Browser encodes file as base64 data URL → server decodes to temp file → `Attachment{LocalPath, ContentType: "image/png"}`
3. **Expert**: `isImageMIME("image/jpeg")` → `true` → `msg.AddImageData(data, "auto", mime)` (full MIME type)
4. **Gemini**: Receives image as `ContentTypeImage` → `genai.NewPartFromBytes(data, format)`
5. **Result**: Model "sees" the image and can describe, analyze, extract text

### Audio (✅ Fully Working)
1. **WhatsApp**: `GetAudioMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "audio/ogg"}`
2. **Expert**: `isAudioMIME("audio/ogg")` → `true` → `addAudioAttachment()`:
   - WAV/MP3: `msg.AddAudioFilePath(path)` directly
   - OGG/other: auto-converts to WAV via `ffmpeg` → `msg.AddAudioFilePath(wavPath)`
   - Fallback (no ffmpeg): `msg.AddFileData(name, data, mime)` — model may still handle it
3. **Gemini**: Receives audio as `ContentTypeAudio` → model understands spoken content
4. **Result**: Model transcribes/understands voice notes and responds accordingly

### Video (✅ Working via File API)
1. **WhatsApp**: `GetVideoMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "video/mp4"}`
2. **Expert**: `isVideoMIME("video/mp4")` → `true` → `addVideoAttachment()`:
   - Reads file bytes → `msg.AddFileData(name, data, "video/mp4")` with explicit MIME
3. **Gemini**: Receives as `ContentTypeFile` with correct MIME → natively processes video
4. **Result**: Model describes motion/action in the video and provides relevant advice

### Documents (✅ Fully Working)
1. **WhatsApp**: `GetDocumentMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "application/pdf"}`
2. **Expert**: Falls through to `msg.AddFilePath(path)` → ✅ Works (`.pdf` in MIME map)
3. **Gemini**: Receives as `ContentTypeFile` → `genai.NewPartFromBytes(data, mime)`

## Arrange

1. Set `GEMINI_API_KEY` in the environment:
   ```bash
   export GEMINI_API_KEY="your-gemini-api-key"
   ```

2. Run Genie with this example config:
   ```bash
   GENIE_CONFIG=examples/multimodal-media-processor/.genie.toml genie grant
   ```

3. **For image testing**: Have image files ready (screenshots, product photos, receipts).

4. **For audio testing**: Requires `ffmpeg` on PATH for OGG→WAV conversion (WhatsApp voice notes). WAV and MP3 files work without ffmpeg.

5. **For video testing**: Requires Gemini model. Video is embedded via `AddFileData()` with explicit MIME type.

6. **Optional — WhatsApp**: Uncomment `[messenger.whatsapp]` in `.genie.toml` and scan the QR code on first run. Then send images, voice notes, and videos from your phone.

7. **Optional — AG-UI**: Use the browser chat interface (`docs/chat.html`) which supports drag-and-drop, paste, and file picker for image/audio/video upload.

## Act

### Scenario 1: Image — Screenshot Debugging
1. Connect to the AG-UI (`docs/chat.html` pointed at port 9876)
2. Send: "What error is shown in this screenshot?" with an attached screenshot of an error dialog
3. The model will describe the error, extract any error codes, and suggest fixes

### Scenario 2: Image + OCR — Receipt Processing
1. Send: "What's the order total on this receipt?" with a photo of a receipt
2. The model uses vision to read the receipt AND can call `ocr_extract_text` for precise text extraction

### Scenario 3: Audio — Voice Complaint (WhatsApp)
1. Send a WhatsApp voice note saying: "Hi, I ordered a laptop last week and the screen is flickering when I open Chrome"
2. The model processes the audio, identifies the issue, and responds with troubleshooting steps

### Scenario 4: Video — Product Demo (WhatsApp + Gemini)
1. Send a short video of a product issue via WhatsApp
2. Gemini analyzes the video content and provides diagnosis

### Scenario 5: Document — Warranty Check
1. Send a PDF of a warranty document with: "Is this product still under warranty?"
2. The model reads the PDF, extracts the coverage dates, and confirms coverage status

## Assert

- **Image**: Model response references specific visual content (colors, text, objects, damage)
- **Audio**: Model understands spoken content and responds to the question asked in the voice note
- **Video**: Model describes motion/action in the video and provides relevant advice
- **Document**: Model extracts specific data points (dates, amounts, names) from the document
- **Urgency**: Every response includes a priority classification (🔴 🟡 🟢)

## Known Gaps (as of current implementation)

> [!NOTE]
> Most multimodal routing gaps have been resolved. The remaining limitations are upstream library constraints.

| Gap | Impact | Workaround |
|-----|--------|------------|
| No `ContentTypeVideo` in trpc-agent-go | Video sent as generic file | Use `AddFileData()` with explicit MIME — works on Gemini |
| No STT tool | Can't transcribe audio to text for non-audio models | Add Whisper-based `speech_to_text` tool |
| OpenAI video support | Video files may not be supported by OpenAI models | Use Gemini for video processing tasks |
