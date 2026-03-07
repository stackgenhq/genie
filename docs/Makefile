# gh-pages local dev server
# Usage:
#   make serve        — start local server on http://localhost:8080
#   make serve PORT=3000  — use a custom port

PORT ?= 9877

.PHONY: serve
serve:
	@echo "🧞 Serving gh-pages at http://localhost:$(PORT)"
	@echo "   Press Ctrl+C to stop."
	@python3 -m http.server $(PORT)
