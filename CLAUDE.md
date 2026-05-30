# CLAUDE.md — http2rdap

This file provides context for AI-assisted development on the `http2rdap` project.

---

## Project overview

`http2rdap` is a single-binary HTTP gateway that exposes RDAP lookups as a JSON REST API.
It is written entirely in Go and embeds all static assets (web UI, favicon, OpenAPI spec) at compile time using `//go:embed` directives, so the resulting binary has zero runtime file dependencies.

The server accepts `POST /api/v1/rdap` requests with a target (domain name, IP address, or AS number) and returns the parsed RDAP record as structured JSON. The authoritative RDAP server is discovered automatically via the IANA bootstrap registry (RFC 9224). Bootstrap files are cached locally in a configurable temporary directory (default: OS temp) with a 24-hour TTL.

---

## Repository layout

```
.
├── api/
│   └── swagger.yaml                          # OpenAPI 3.0 source (human-editable)
├── build/
│   └── Dockerfile                            # Two-stage Docker build (builder + scratch runtime)
├── cmd/
│   └── http2rdap/
│       ├── main.go                           # Entire HTTP server — single file
│       └── static/
│           ├── favicon.png                   # Embedded at build time
│           ├── index.html                    # Embedded web UI (dark/light, 15 languages)
│           └── openapi.json                  # Embedded OpenAPI spec (generated from swagger.yaml)
├── internal/
│   └── rdapclient/
│       ├── rdapclient.go                     # Public API: Client, New(), Lookup(), functional options
│       ├── bootstrap/
│       │   └── bootstrap.go                  # IANA bootstrap resolver (RFC 9224) with disk cache
│       └── result/
│           └── result.go                     # Result and Entity struct definitions
├── scripts/
│   ├── 000_init.sh                           # go mod tidy
│   ├── 999_test.sh                           # Integration smoke tests (curl + jq)
│   ├── linux_build.sh                        # Native static binary build
│   ├── linux_run.sh                          # Run binary on Linux
│   ├── docker_build.sh                       # Build Docker image
│   ├── docker_run.sh                         # Run Docker container
│   ├── windows_build.cmd                     # Native build on Windows
│   └── windows_run.cmd                       # Run binary on Windows
├── go.mod
├── LICENSE                                   # Apache 2.0
├── README.md
└── CLAUDE.md                                 # This file
```

---

## Key design decisions

- **Single `main.go` for the server**: the entire HTTP server logic lives in `cmd/http2rdap/main.go`. Keep it that way unless the file grows substantially.
- **Separate `internal/rdapclient` package**: RDAP client logic is fully decoupled from the HTTP layer. It exposes a clean public API (`Client`, `New()`, `Lookup()`, `WithTimeout()`) and can be imported independently.
- **Embedded assets**: `favicon.png`, `index.html`, and `openapi.json` are embedded with `//go:embed`. Any change to these files is picked up at the next `go build` — no copy step needed.
- **Static binary**: the build uses `-tags netgo` and `-ldflags "-extldflags -static"` to produce a fully self-contained binary with no libc dependency. Do not introduce `cgo` dependencies.
- **No framework**: the HTTP layer uses only the standard library (`net/http`). Do not add a router or web framework.
- **No external dependencies**: the RDAP client uses only the Go standard library.
- **IANA bootstrap caching**: `internal/rdapclient/bootstrap` fetches the four IANA bootstrap files (`dns.json`, `ipv4.json`, `ipv6.json`, `asn.json`) and caches them in `TEMP_DIR` with a 24-hour TTL. Stale cache is used as a fallback if a fresh fetch fails.
- **Handler factory**: `makeRDAPHandler` returns an `http.HandlerFunc` closed over the resolved `config` and the default `Client` instance. This avoids package-level mutable state and simplifies testing.
- **RDAP protocol**: queries are sent as HTTPS GET requests to the authoritative RDAP server discovered via bootstrap. The response is parsed from JSON (RFC 9083), including vCard 4.0 entity records.

---

## Environment variables & CLI flags

Every configuration value can be set via an environment variable **or** a command-line flag. The flag always takes priority. Resolution order: **CLI flag -> environment variable -> hard-coded default**.

| Environment variable | CLI flag            | Default           | Description                                                                 |
|----------------------|---------------------|-------------------|-----------------------------------------------------------------------------|
| `LISTEN_ADDR`        | `-addr`             | `127.0.0.1:8080`  | Listen address (`host:port`).                                               |
| `RDAP_TIMEOUT`       | `-rdap-timeout`     | `15s`             | Per-query RDAP HTTP timeout. Accepts Go duration strings (e.g. `30s`).      |
| `REQUEST_TIMEOUT`    | `-request-timeout`  | `20s`             | Global HTTP request deadline.                                               |
| `TEMP_DIR`           | `-temp-dir`         | `""` (OS default) | Directory for IANA bootstrap file cache. Empty string uses `os.TempDir()`. |

CLI flags are parsed with the standard library `flag` package. Flags use `""` / `0` as a sentinel value (not the real default) to detect whether a flag was explicitly passed, without relying on `flag.Visit`. Any new configuration entry must expose both a flag and its environment variable counterpart, following the same three-step resolution pattern in `resolveConfig()`.

---

## Build & run commands

```bash
# Initialise / tidy dependencies
bash scripts/000_init.sh

# Build native static binary -> ./out/http2rdap
bash scripts/linux_build.sh

# Run (sets LISTEN_ADDR=0.0.0.0:8080)
bash scripts/linux_run.sh

# Build Docker image -> letstool/http2rdap:latest
bash scripts/docker_build.sh

# Run Docker container
bash scripts/docker_run.sh

# Smoke tests (server must be running)
bash scripts/999_test.sh
```

---

## API contract

### Endpoint

```
POST /api/v1/rdap
Content-Type: application/json
```

### Request fields

Exactly one of `ip`, `domain`, or `asn` must be provided per request.

| Field     | Type     | Required | Notes                                                                          |
|-----------|----------|----------|--------------------------------------------------------------------------------|
| `ip`      | `string` | (*)      | IPv4 or IPv6 address (e.g. `8.8.8.8`, `2001:4860:4860::8888`)                 |
| `domain`  | `string` | (*)      | Domain name (e.g. `example.com`)                                               |
| `asn`     | `string` | (*)      | AS number, with or without `AS` prefix (e.g. `15169` or `AS15169`)            |
| `timeout` | `int`    | no       | Per-request RDAP timeout in seconds (`0` uses the server default).             |

(*) Exactly one of these three fields is required.

### Response status values

| Value       | HTTP | Meaning                                                       |
|-------------|------|---------------------------------------------------------------|
| `SUCCESS`   | 200  | Query resolved — `answers` contains the parsed RDAP result   |
| `NOTFOUND`  | 200  | No RDAP record found for the target                          |
| `ERROR`     | 200  | Bad request, invalid input, or network failure               |
| `RATELIMIT` | 429  | The authoritative RDAP server returned HTTP 429 Too Many Requests |

### Other endpoints

| Method | Path            | Description                        |
|--------|-----------------|------------------------------------|
| `GET`  | `/`             | Embedded interactive web UI        |
| `GET`  | `/openapi.json` | OpenAPI 3.0 specification          |
| `GET`  | `/favicon.png`  | Application icon                   |

---

## Internal package responsibilities

### `internal/rdapclient` (rdapclient.go)

Public-facing API. Exposes `Client`, `New()`, `Lookup()`, `WithTimeout()`, and the sentinel error type `ErrRateLimit`. Handles query-type detection (domain / IPv4 / IPv6 / ASN), calls the bootstrap resolver to find the authoritative RDAP server, performs the HTTPS GET request, and parses the RFC 9083 JSON response including vCard 4.0 entity records. Returns `*ErrRateLimit` (detectable via `IsRateLimit()` or `errors.As`) when the RDAP server responds with HTTP 429.

### `internal/rdapclient/bootstrap` (bootstrap.go)

IANA bootstrap resolver (RFC 9224). Downloads and caches the four bootstrap files (`dns.json`, `ipv4.json`, `ipv6.json`, `asn.json`) from `https://data.iana.org/rdap/`. Files are cached in `TEMP_DIR` with a 24-hour TTL. Stale cache is used as a fallback on network failure. Provides `ForDomain()`, `ForIP()`, and `ForASN()` methods, each returning the best-matching authoritative RDAP server URL.

### `internal/rdapclient/result` (result.go)

Defines the `Result` and `Entity` structs. `Result` covers all RDAP object types: domain fields (ldhName, unicodeName, nameServers, delegationSigned), IP network fields (startAddress, endAddress, ipVersion, CIDRs), autnum fields (startAutnum, endAutnum), shared fields (handle, status, dates, name, type, country), contact pointers, remarks, and raw RDAP JSON. All fields are JSON-tagged with `omitempty`.

---

## Web UI

The UI is a self-contained single-file HTML/JS/CSS application embedded in the binary.

- **Themes**: dark and light, switchable via a toggle button.
- **Languages**: 15 locales built in — Arabic (`ar`), Bengali (`bn`), Chinese (`zh`), German (`de`), English (`en`), Spanish (`es`), French (`fr`), Hindi (`hi`), Indonesian (`id`), Japanese (`ja`), Korean (`ko`), Portuguese (`pt`), Russian (`ru`), Urdu (`ur`), Vietnamese (`vi`). Language is selected from a dropdown.
- **RTL support**: Arabic and Urdu automatically switch the layout to right-to-left.
- The UI calls `POST /api/v1/rdap` and renders results in structured sections (general info, registration dates, name servers, contacts, raw RDAP JSON).
- The OpenAPI spec is also served at `/openapi.json` for use with tools such as Swagger UI or Postman.

To modify the UI, edit `cmd/http2rdap/static/index.html` and rebuild.
To update the API spec, edit `api/swagger.yaml`, regenerate `openapi.json` (e.g. with `python3 -c "import yaml,json; json.dump(yaml.safe_load(open('api/swagger.yaml')), open('cmd/http2rdap/static/openapi.json','w'), indent=2)"`), and rebuild.

---

## Constraints & conventions

- Go version: **1.24+**
- No `cgo`. Keep `CGO_ENABLED=0`.
- No additional HTTP frameworks or routers.
- All HTTP server logic stays in `cmd/http2rdap/main.go` unless a strong reason arises to split it.
- All RDAP client logic stays in `internal/rdapclient` and its sub-packages.
- Error responses always return a `RDAPResponse` JSON body — never a plain-text error.
- The server never logs request bodies; avoid adding logging that could expose user-queried targets.
- All code, identifiers, comments, and documentation must be written in **English**. No icons, emoji, or non-ASCII decorations in comments or doc strings.
- **Every configuration environment variable must have a corresponding command-line flag** (parsed via `flag` from the standard library). The flag always takes priority over the environment variable. The resolution order is: CLI flag -> environment variable -> hard-coded default. New entries must follow the sentinel pattern used in `resolveConfig()` and be documented in the table above.

---

## AI-assisted development

This project was developed with the assistance of **Claude Sonnet 4.6** by Anthropic.

Migrated from `http2whois` (WHOIS gateway) to `http2rdap` (RDAP gateway) by **Claude Sonnet 4.6** by Anthropic.
