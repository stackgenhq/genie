package expert

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// This file tests the multimodal attachment routing logic in buildUserMessage.
// It uses an internal test (package expert, not expert_test) to access the
// unexported functions: isImageMIME, isAudioMIME, isVideoMIME, buildUserMessage.

var _ = Describe("Multimodal Attachment Routing", func() {
	Describe("isImageMIME", func() {
		It("returns true for image content types", func() {
			Expect(isImageMIME("image/png")).To(BeTrue())
			Expect(isImageMIME("image/jpeg")).To(BeTrue())
			Expect(isImageMIME("image/webp")).To(BeTrue())
			Expect(isImageMIME("image/gif")).To(BeTrue())
			Expect(isImageMIME("image/svg+xml")).To(BeTrue())
		})

		It("returns false for non-image content types", func() {
			Expect(isImageMIME("audio/mpeg")).To(BeFalse())
			Expect(isImageMIME("video/mp4")).To(BeFalse())
			Expect(isImageMIME("application/pdf")).To(BeFalse())
			Expect(isImageMIME("text/plain")).To(BeFalse())
			Expect(isImageMIME("")).To(BeFalse())
		})
	})

	Describe("isAudioMIME", func() {
		It("returns true for audio content types", func() {
			Expect(isAudioMIME("audio/mpeg")).To(BeTrue())
			Expect(isAudioMIME("audio/wav")).To(BeTrue())
			Expect(isAudioMIME("audio/ogg")).To(BeTrue())
			Expect(isAudioMIME("audio/webm")).To(BeTrue())
			Expect(isAudioMIME("audio/mp4")).To(BeTrue())
		})

		It("returns false for non-audio content types", func() {
			Expect(isAudioMIME("image/png")).To(BeFalse())
			Expect(isAudioMIME("video/mp4")).To(BeFalse())
			Expect(isAudioMIME("application/pdf")).To(BeFalse())
			Expect(isAudioMIME("")).To(BeFalse())
		})
	})

	Describe("isVideoMIME", func() {
		It("returns true for video content types", func() {
			Expect(isVideoMIME("video/mp4")).To(BeTrue())
			Expect(isVideoMIME("video/webm")).To(BeTrue())
			Expect(isVideoMIME("video/quicktime")).To(BeTrue())
		})

		It("returns false for non-video content types", func() {
			Expect(isVideoMIME("image/png")).To(BeFalse())
			Expect(isVideoMIME("audio/mpeg")).To(BeFalse())
			Expect(isVideoMIME("application/pdf")).To(BeFalse())
			Expect(isVideoMIME("")).To(BeFalse())
		})
	})

	Describe("inferVideoMIME", func() {
		It("returns correct MIME for known extensions", func() {
			Expect(inferVideoMIME("/tmp/video.mp4")).To(Equal("video/mp4"))
			Expect(inferVideoMIME("/tmp/video.webm")).To(Equal("video/webm"))
			Expect(inferVideoMIME("/tmp/video.mov")).To(Equal("video/quicktime"))
			Expect(inferVideoMIME("/tmp/video.avi")).To(Equal("video/x-msvideo"))
			Expect(inferVideoMIME("/tmp/video.mkv")).To(Equal("video/x-matroska"))
		})

		It("returns video/mp4 as default for unknown extensions", func() {
			Expect(inferVideoMIME("/tmp/video.xyz")).To(Equal("video/mp4"))
		})
	})

	Describe("buildUserMessage", func() {
		var tmpDir string

		BeforeEach(func() {
			var err error
			tmpDir, err = os.MkdirTemp("", "expert-multimodal-test-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			os.RemoveAll(tmpDir)
		})

		It("returns a text-only message when no attachments", func() {
			req := Request{
				Message: "Hello, world!",
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.Content).To(Equal("Hello, world!"))
			Expect(msg.ContentParts).To(BeEmpty())
		})

		It("skips attachments without LocalPath", func() {
			req := Request{
				Message: "Check this",
				Attachments: []messenger.Attachment{
					{
						Name:        "screenshot.png",
						ContentType: "image/png",
						URL:         "https://example.com/screenshot.png",
						// No LocalPath — should be skipped.
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.Content).To(Equal("Check this"))
			Expect(msg.ContentParts).To(BeEmpty())
		})

		It("embeds image attachments as image content parts", func() {
			// Create a minimal PNG file (1x1 pixel).
			imgPath := filepath.Join(tmpDir, "test.png")
			// Minimal valid PNG: 8-byte header + IHDR + IDAT + IEND
			pngData := []byte{
				0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
				0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
				0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
				0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // rgb, no interlace
				0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, // IDAT chunk
				0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, // compressed data
				0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC, // ...
				0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, // IEND chunk
				0x44, 0xAE, 0x42, 0x60, 0x82,
			}
			Expect(os.WriteFile(imgPath, pngData, 0o644)).To(Succeed())

			req := Request{
				Message: "What's in this image?",
				Attachments: []messenger.Attachment{
					{
						Name:        "test.png",
						ContentType: "image/png",
						LocalPath:   imgPath,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.Content).To(Equal("What's in this image?"))
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeImage))
			Expect(msg.ContentParts[0].Image).NotTo(BeNil())
		})

		It("embeds WAV audio attachments as audio content parts", func() {
			// Create a minimal WAV file (RIFF header + empty data).
			wavPath := filepath.Join(tmpDir, "voice.wav")
			wavData := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3E\x00\x00\x00\x7D\x00\x00\x02\x00\x10\x00data\x00\x00\x00\x00")
			Expect(os.WriteFile(wavPath, wavData, 0o644)).To(Succeed())

			req := Request{
				Message: "What did I say?",
				Attachments: []messenger.Attachment{
					{
						Name:        "voice.wav",
						ContentType: "audio/wav",
						LocalPath:   wavPath,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.Content).To(Equal("What did I say?"))
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeAudio))
			Expect(msg.ContentParts[0].Audio).NotTo(BeNil())
			Expect(msg.ContentParts[0].Audio.Format).To(Equal("wav"))
		})

		It("embeds MP3 audio attachments as audio content parts", func() {
			mp3Path := filepath.Join(tmpDir, "voice.mp3")
			// Minimal valid MP3 frame header (just enough to not error on extension check).
			mp3Data := []byte{0xFF, 0xFB, 0x90, 0x00, 0x00}
			Expect(os.WriteFile(mp3Path, mp3Data, 0o644)).To(Succeed())

			req := Request{
				Message: "Transcribe this",
				Attachments: []messenger.Attachment{
					{
						Name:        "voice.mp3",
						ContentType: "audio/mpeg",
						LocalPath:   mp3Path,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeAudio))
		})

		It("embeds video attachments as file content parts with explicit MIME", func() {
			videoPath := filepath.Join(tmpDir, "demo.mp4")
			videoData := []byte("fake-video-data-for-testing")
			Expect(os.WriteFile(videoPath, videoData, 0o644)).To(Succeed())

			req := Request{
				Message: "What's happening in this video?",
				Attachments: []messenger.Attachment{
					{
						Name:        "demo.mp4",
						ContentType: "video/mp4",
						LocalPath:   videoPath,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeFile))
			Expect(msg.ContentParts[0].File).NotTo(BeNil())
			Expect(msg.ContentParts[0].File.MimeType).To(Equal("video/mp4"))
			Expect(msg.ContentParts[0].File.Name).To(Equal("demo.mp4"))
		})

		It("embeds PDF attachments via AddFilePath", func() {
			pdfPath := filepath.Join(tmpDir, "receipt.pdf")
			// Minimal PDF.
			pdfData := []byte("%PDF-1.0\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]>>endobj\nxref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n190\n%%EOF")
			Expect(os.WriteFile(pdfPath, pdfData, 0o644)).To(Succeed())

			req := Request{
				Message: "Summarize this document",
				Attachments: []messenger.Attachment{
					{
						Name:        "receipt.pdf",
						ContentType: "application/pdf",
						LocalPath:   pdfPath,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeFile))
			Expect(msg.ContentParts[0].File).NotTo(BeNil())
			Expect(msg.ContentParts[0].File.MimeType).To(Equal("application/pdf"))
		})

		It("handles OGG audio by falling back to file data when ffmpeg is unavailable", func() {
			oggPath := filepath.Join(tmpDir, "voice.ogg")
			oggData := []byte("OggS\x00\x02fake-ogg-data")
			Expect(os.WriteFile(oggPath, oggData, 0o644)).To(Succeed())

			req := Request{
				Message: "What did the customer say?",
				Attachments: []messenger.Attachment{
					{
						Name:        "voice.ogg",
						ContentType: "audio/ogg",
						LocalPath:   oggPath,
					},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			// Without ffmpeg, should fall back to AddFileData with explicit MIME.
			Expect(msg.ContentParts).To(HaveLen(1))
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeFile))
			Expect(msg.ContentParts[0].File).NotTo(BeNil())
			Expect(msg.ContentParts[0].File.MimeType).To(Equal("audio/ogg"))
		})

		It("handles multiple mixed attachments", func() {
			// Create image.
			imgPath := filepath.Join(tmpDir, "photo.png")
			pngData := []byte{
				0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
				0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
				0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
				0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
				0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
				0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
				0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC,
				0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
				0x44, 0xAE, 0x42, 0x60, 0x82,
			}
			Expect(os.WriteFile(imgPath, pngData, 0o644)).To(Succeed())

			// Create video.
			vidPath := filepath.Join(tmpDir, "clip.mp4")
			Expect(os.WriteFile(vidPath, []byte("fake-video"), 0o644)).To(Succeed())

			// Create audio.
			wavPath := filepath.Join(tmpDir, "note.wav")
			wavData := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3E\x00\x00\x00\x7D\x00\x00\x02\x00\x10\x00data\x00\x00\x00\x00")
			Expect(os.WriteFile(wavPath, wavData, 0o644)).To(Succeed())

			req := Request{
				Message: "Multiple media",
				Attachments: []messenger.Attachment{
					{Name: "photo.png", ContentType: "image/png", LocalPath: imgPath},
					{Name: "clip.mp4", ContentType: "video/mp4", LocalPath: vidPath},
					{Name: "note.wav", ContentType: "audio/wav", LocalPath: wavPath},
				},
			}
			msg := buildUserMessage(context.Background(), req)
			Expect(msg.ContentParts).To(HaveLen(3))

			// Image → ContentTypeImage
			Expect(msg.ContentParts[0].Type).To(Equal(model.ContentTypeImage))
			// Video → ContentTypeFile (with video MIME)
			Expect(msg.ContentParts[1].Type).To(Equal(model.ContentTypeFile))
			Expect(msg.ContentParts[1].File.MimeType).To(Equal("video/mp4"))
			// Audio → ContentTypeAudio
			Expect(msg.ContentParts[2].Type).To(Equal(model.ContentTypeAudio))
		})
	})

	Describe("readFileBytes", func() {
		It("returns file contents for valid path", func() {
			tmpFile, err := os.CreateTemp("", "readfile-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.Write([]byte("hello multimodal"))
			Expect(err).NotTo(HaveOccurred())
			tmpFile.Close()

			data := readFileBytes(tmpFile.Name())
			Expect(data).To(Equal([]byte("hello multimodal")))
		})

		It("returns nil for non-existent path", func() {
			data := readFileBytes("/nonexistent/path/file.bin")
			Expect(data).To(BeNil())
		})
	})

	Describe("convertAudioToWAV", func() {
		It("returns error when ffmpeg is not available", func() {
			// This test relies on the system not having ffmpeg at an impossible path.
			// We test the LookPath check by providing a valid source path.
			tmpFile, err := os.CreateTemp("", "audio-test-*.ogg")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(tmpFile.Name())
			tmpFile.Write([]byte("fake-ogg"))
			tmpFile.Close()

			// If ffmpeg is available on the system, this test will still pass
			// since the input is not valid OGG — ffmpeg will fail on the content.
			_, err = convertAudioToWAV(context.Background(), tmpFile.Name())
			// Either ffmpeg not found OR ffmpeg fails on invalid data — both are acceptable.
			Expect(err).To(HaveOccurred())
		})
	})
})

func TestMultimodal(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Expert Multimodal Suite")
}
