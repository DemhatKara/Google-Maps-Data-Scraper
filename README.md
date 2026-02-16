# Google Maps Scraper

A high-performance Google Maps scraper with a built-in web dashboard. Extract business data — names, addresses, phone numbers, websites, ratings, reviews, emails, and 30+ fields — from Google Maps at scale.

![Dashboard](https://img.shields.io/badge/Dashboard-Web%20UI-blue)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)

## Features

- **Web Dashboard** — Point-and-click interface to configure and launch scraping jobs
- **Fast Mode** — HTTP-based scraping (no browser) with automatic pagination, up to 120 results per query
- **Normal Mode** — Full Playwright browser automation with infinite scroll
- **34+ Data Fields** — Business name, address, phone, website, category, ratings, reviews, hours, emails, coordinates, and more
- **Built-in Deduplication** — CID-based deduplication eliminates duplicate results automatically
- **Per-Query Geolocation** — Each query uses its own city coordinates for accurate local results
- **Proxy Support** — HTTP, HTTPS, and SOCKS5 proxy support with authentication
- **Export Formats** — Download results as CSV or Excel (XLSX) with customizable field selection
- **Multi-Language** — Scrape in English, Turkish, German, French, or Spanish
- **Docker Ready** — Multi-stage Dockerfile with Playwright + Chromium included
- **REST API** — Full API with OpenAPI/Swagger documentation

## Quick Start

### Option 1: Docker (Recommended)

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080) in your browser.

### Option 2: Build from Source

**Prerequisites:** Go 1.25+ and Playwright Chromium

```bash
# Build
go build -o google-maps-scraper .

# Install Playwright browsers (first time only)
./google-maps-scraper -install-playwright

# Run the web dashboard
./google-maps-scraper -web -c 5 -addr :8080
```

Open [http://localhost:8080](http://localhost:8080) in your browser.

### Option 3: CLI Mode

```bash
# Create a file with search queries (one per line)
echo "coffee shop New York" > queries.txt
echo "pizza restaurant Chicago" >> queries.txt

# Run scraper with file input
./google-maps-scraper -input queries.txt -results output.csv -lang en -c 3
```

## Web Dashboard

The dashboard provides a complete interface for managing scraping jobs:

1. **Enter keywords** — one search query per line (e.g., "coffee shop New York")
2. **Select language**, zoom level, and search radius
3. **Choose data fields** you want to extract
4. **Configure proxies** if needed
5. **Start scraping** and monitor progress in real time
6. **Download results** as CSV or Excel

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-web` | `false` | Run web dashboard mode |
| `-addr` | `:8080` | Web server listen address |
| `-c` | `3` | Concurrency (parallel workers) |
| `-input` | | Input file with queries (one per line) |
| `-results` | `stdout` | Output file path |
| `-lang` | `en` | Language code (en, tr, de, fr, es) |
| `-fast-mode` | `false` | Use HTTP-based fast scraping (no browser) |
| `-depth` | `10` | Max scroll depth / pagination pages |
| `-zoom` | `15` | Google Maps zoom level (0–21) |
| `-radius` | `10000` | Search radius in meters |
| `-geo` | | Geo coordinates (`lat,lon`) |
| `-email` | `false` | Extract emails from business websites |
| `-proxies` | | Comma-separated proxy list |
| `-json` | `false` | Output JSON instead of CSV |
| `-debug` | `false` | Headful browser mode (visible window) |
| `-data-folder` | `webdata` | Data storage directory |

## Extracted Data Fields

| Field | Description |
|-------|-------------|
| `title` | Business name |
| `category` | Business category |
| `address` | Street address |
| `phone` | Phone number |
| `website` | Website URL |
| `review_count` | Number of reviews |
| `review_rating` | Average rating (1–5) |
| `open_hours` | Working hours |
| `latitude` / `longitude` | GPS coordinates |
| `link` | Google Maps URL |
| `emails` | Extracted email addresses |
| `plus_code` | Google Plus Code |
| `status` | Business status |
| `price_range` | Price level |
| `descriptions` | Business description |
| `popular_times` | Popular visiting times |
| `about` | About section |
| `owner` | Business owner |
| `complete_address` | Full structured address |
| `reviews_per_rating` | Review breakdown by stars |
| `thumbnail` / `images` | Business photos |
| `user_reviews` | Individual review texts |
| `reservations` | Reservation links |
| `order_online` | Online ordering links |
| `menu` | Menu link |
| `timezone` | Business timezone |

## Architecture

```
├── main.go                 # Entry point, runner factory
├── gmaps/                  # Scraping engine
│   ├── job.go              # Normal mode (Playwright browser)
│   ├── searchjob.go        # Fast mode (HTTP API + pagination)
│   ├── place.go            # Place detail extraction
│   ├── entry.go            # Data model (34 fields)
│   ├── emailjob.go         # Email extraction
│   └── reviews.go          # Review parsing
├── web/                    # Web dashboard + REST API
│   ├── web.go              # HTTP server & routes
│   ├── service.go          # Business logic (CRUD, export)
│   └── sqlite/             # SQLite job storage
├── runner/                 # Execution modes
│   ├── webrunner/          # Dashboard mode
│   ├── filerunner/         # CLI file mode
│   └── databaserunner/     # PostgreSQL mode
├── deduper/                # FNV64 hash-based deduplication
├── Dockerfile              # Multi-stage Docker build
└── docker-compose.yml      # One-command deployment
```

## Proxy Configuration

Supports HTTP, HTTPS, and SOCKS5 proxies:

```
http://username:password@proxy.example.com:8080
https://username:password@proxy.example.com:443
socks5://127.0.0.1:9050
```

Add proxies in the dashboard or via CLI:

```bash
./google-maps-scraper -input queries.txt -proxies "http://user:pass@host:port"
```

## API

The REST API is available at `/api/docs` when running in web mode. It supports:

- `POST /scrape` — Start a new scraping job
- `GET /jobs` — List all jobs
- `GET /download?id={id}` — Download results (CSV/Excel)
- `DELETE /delete?id={id}` — Delete a job

## License

MIT
