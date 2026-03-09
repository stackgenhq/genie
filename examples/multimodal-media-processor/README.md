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
                    │             │  • image/* → AddImageFilePath()
                    │             │  • audio/* → AddAudioFilePath()  ← GAP: not wired yet
                    │             │  • video/* → AddFilePath()       ← via File content type
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
2. **Expert**: `isImageMIME("image/jpeg")` → `true` → `msg.AddImageFilePath(path, "auto")`
3. **Gemini**: Receives image as `ContentTypeImage` → `genai.NewPartFromBytes(data, format)`
4. **Result**: Model "sees" the image and can describe, analyze, extract text

### Audio (⚠️ Partially Working — See Gaps)
1. **WhatsApp**: `GetAudioMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "audio/ogg"}`
2. **Expert**: `isImageMIME("audio/ogg")` → `false` → `msg.AddFilePath(path)` → ⚠️ **fails** (`.ogg` not in MIME map)
3. **Gap**: Audio should be routed to `AddAudioFilePath()`, and OGG needs conversion to WAV/MP3

### Video (⚠️ Partially Working via File API)
1. **WhatsApp**: `GetVideoMessage()` → `downloadAndSave()` → `Attachment{LocalPath, ContentType: "video/mp4"}`
2. **Expert**: Falls through to `msg.AddFilePath(path)` → ⚠️ **fails** (`.mp4` not in MIME map)
3. **Gap**: Needs `AddFileData()` with explicit MIME, or upstream `ContentTypeVideo` support

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

4. **For audio testing**: Requires the `buildUserMessage` fix (see Known Gaps below) OR use WhatsApp which downloads audio to local path.

5. **For video testing**: Requires Gemini model and the video routing fix.

6. **Optional — WhatsApp**: Uncomment `[messenger.whatsapp]` in `.genie.toml` and scan the QR code on first run. Then send images, voice notes, and videos from your phone.

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

> [!WARNING]
> The following gaps exist in the current Genie + trpc-agent-go stack. See the full [gap analysis](/Users/sabithks/.gemini/antigravity/brain/d1944d7b-b2c9-4ef6-9850-6e837ade2f97/multimodal_gap_analysis.md) for details.

| Gap | Impact | Workaround |
|-----|--------|------------|
| Audio not routed to `AddAudioFilePath` | Voice messages fail as file input | Fix `buildUserMessage()` to check `isAudioMIME()` |
| OGG format not supported | WhatsApp voice notes rejected | Convert OGG→WAV via ffmpeg before sending to model |
| No `ContentTypeVideo` in trpc-agent-go | Video sent as generic file | Use `AddFileData()` with explicit MIME — works on Gemini |
| AG-UI has no file upload | Can't test multimodal from browser | Use WhatsApp, or add upload endpoint to AG-UI |
| No STT tool | Can't transcribe audio to text for non-audio models | Add Whisper-based `speech_to_text` tool |
