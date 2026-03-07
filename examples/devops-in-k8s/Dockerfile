FROM ghcr.io/stackgenhq/genie:latest

# Install aws-cli using apk since this is an Alpine image
USER root
RUN apk add --no-cache aws-cli jq curl bash
USER stackgen
