# Build stage
FROM golang:1.25.6-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Ensure dependencies are tidy
RUN go mod tidy
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /go/bin/google-maps-scraper .

# Playwright stage to get browsers
FROM mcr.microsoft.com/playwright:v1.49.0-noble AS playwright
RUN mkdir -p /opt/browsers
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
RUN npx playwright install chromium

# Final stage
FROM debian:bookworm-slim

# Install runtime dependencies for chromium
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libdbus-1-3 \
    libxkbcommon0 \
    libx11-6 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libpango-1.0-0 \
    libcairo2 \
    libasound2 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Set up environment
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
COPY --from=playwright /opt/browsers /opt/browsers
COPY --from=builder /go/bin/google-maps-scraper /usr/local/bin/google-maps-scraper

WORKDIR /app

# Ensure entrypoint is the binary
ENTRYPOINT ["google-maps-scraper"]
# Default command (can be overridden)
CMD ["-fast-mode"]
