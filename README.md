# http2rdap

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Docker Pulls](https://img.shields.io/docker/pulls/letstool/http2rdap)](https://hub.docker.com/r/letstool/http2rdap)

> **RDAP-over-HTTP** — Fast & lightweight HTTP gateway that serves RDAP queries as a JSON REST API.

Built in Go, it accepts a `POST` request with a target (domain, IP address or ASN) and returns the parsed RDAP record as structured JSON. The authoritative RDAP server is discovered automatically via the IANA bootstrap registry (RFC 9224). The binary embeds a static web UI and an OpenAPI specification, with zero runtime dependencies.

---

## Screenshot

![http2rdap Web UI](docs/screenshot.png)

> The embedded web UI (served at `/`) provides an interactive form to issue RDAP queries and inspect responses directly in the browser. It supports **dark and light themes** and is fully translated into **15 languages**.

---

## Releases

Provided binaries are fully functional natively on Linux (amd64 or arm64), Windows (amd64), macOS (amd64 or arm64), and also via Docker (amd64 or arm64), with no additional dependencies required. For download and installation, please refer to the [Releases](https://github.com/letstool/http2rdap/releases) page.

---

## Disclaimer

This project is released **as-is**, for demonstration or reference purposes.
It is **not maintained**: no bug fixes, dependency updates, or new features are planned. Issues and pull requests will not be addressed.

---

## License

This project is licensed under the **Apache License, Version 2.0** — see the [`LICENSE`](LICENSE) file for details.

```
Copyright (c) 2026 letstool.net
```

---

## Features

- Single static binary — no external runtime dependencies
- Embedded web UI and OpenAPI 3.0 specification (`/openapi.json`)
- Web UI available in **dark and light mode**, switchable at runtime via a toggle
- Web UI fully translated into **15 languages**: Arabic, Bengali, Chinese, German, English, Spanish, French, Hindi, Indonesian, Japanese, Korean, Portuguese, Russian, Urdu, Vietnamese
- Supports **domain names**, **IPv4 / IPv6 addresses**, and **AS numbers**
- Automatic RDAP server discovery via the **IANA bootstrap registry** (RFC 9224)
- Bootstrap data cached locally with a **24-hour TTL** (configurable directory via `TEMP_DIR`)
- Returns fully **structured and parsed** RDAP data (registrar, dates, name servers, contacts, network ranges, DNSSEC, …)
- Raw RDAP JSON included alongside parsed fields
- Configurable listen address, RDAP timeout, and HTTP request deadline
- Docker image built on `scratch` — minimal attack surface

---

## Build

### Prerequisites

- [Go](https://go.dev/dl/) **1.24+**

### Native binary (Linux)

```bash
bash scripts/linux_build.sh
```

The binary is output to `./out/http2rdap`.

The script produces a **fully static binary** (no libc dependency):

```bash
go build \
    -mod=vendor \
    -trimpath \
    -ldflags="-extldflags -static -s -w" \
    -tags netgo \
    -o ./out/http2rdap ./cmd/http2rdap
```

### Windows

```cmd
scripts\windows_build.cmd
```

### Docker image

```bash
bash scripts/docker_build.sh
```

This runs a two-stage Docker build:

1. **Builder** — `golang:1.24-alpine` compiles a static binary
2. **Runtime** — `scratch` image, containing only the binary

The resulting image is tagged `letstool/http2rdap:latest`.

---

## Run

### Native (Linux)

```bash
bash scripts/linux_run.sh
```

This sets `LISTEN_ADDR=0.0.0.0:8080` and runs the binary.

### Windows

```cmd
scripts\windows_run.cmd
```

### Docker

```bash
bash scripts/docker_run.sh
```

Equivalent to:

```bash
docker run -it --rm -p 8080:8080 \
  -e LISTEN_ADDR=0.0.0.0:8080 \
  letstool/http2rdap:latest
```

Once running, the service is available at [http://localhost:8080/](http://localhost:8080/).

---

## Configuration

Each setting can be provided as a CLI flag or an environment variable. The CLI flag always takes priority. Resolution order: **CLI flag → environment variable → default**.

| CLI flag            | Environment variable | Default           | Description                                                                  |
|---------------------|----------------------|-------------------|------------------------------------------------------------------------------|
| `-addr`             | `LISTEN_ADDR`        | `127.0.0.1:8080`  | Address and port the HTTP server listens on.                                 |
| `-rdap-timeout`     | `RDAP_TIMEOUT`       | `15s`             | Per-query RDAP HTTP timeout. Accepts Go duration strings (e.g. `30s`).       |
| `-request-timeout`  | `REQUEST_TIMEOUT`    | `20s`             | Global HTTP request deadline.                                                |
| `-temp-dir`         | `TEMP_DIR`           | `""` (OS default) | Directory for IANA bootstrap file cache. Empty string uses `os.TempDir()`.  |

**Examples:**

```bash
# Using CLI flags
./out/http2rdap -addr 0.0.0.0:9090 -rdap-timeout 30s -request-timeout 45s

# Using environment variables
LISTEN_ADDR=0.0.0.0:9090 RDAP_TIMEOUT=30s REQUEST_TIMEOUT=45s TEMP_DIR=/var/cache/rdap ./out/http2rdap

# Mixed: CLI flag overrides the environment variable for addr only
LISTEN_ADDR=0.0.0.0:9090 ./out/http2rdap -addr 127.0.0.1:8080
```

---

## API Reference

### `POST /api/v1/rdap`

Performs an RDAP lookup and returns parsed structured data. The authoritative RDAP server is discovered automatically via the IANA bootstrap registry (RFC 9224).

Exactly one of `ip`, `domain`, or `asn` must be provided per request.

#### Request body

```json
{
  "domain": "example.com",
  "timeout": 10
}
```

| Field     | Type      | Required | Description                                                                                   |
|-----------|-----------|----------|-----------------------------------------------------------------------------------------------|
| `ip`      | `string`  | ❌       | IPv4 or IPv6 address to look up (e.g. `8.8.8.8`, `2001:4860:4860::8888`)                     |
| `domain`  | `string`  | ❌       | Domain name to look up (e.g. `example.com`)                                                   |
| `asn`     | `string`  | ❌       | Autonomous System Number, with or without the `AS` prefix (e.g. `15169` or `AS15169`)        |
| `timeout` | `integer` | ❌       | Per-request RDAP timeout in seconds (`0` uses the server default).                            |

#### Response body

```json
{
  "status": "SUCCESS",
  "answers": {
    "query": "example.com",
    "queryType": "domain",
    "rdapServer": "https://rdap.verisign.com/com/v1/",
    "handle": "2336799_DOMAIN_COM-VRSN",
    "ldhName": "example.com",
    "unicodeName": "example.com",
    "status": ["active"],
    "nameServers": ["a.iana-servers.net", "b.iana-servers.net"],
    "delegationSigned": false,
    "created": "1995-08-14T04:00:00Z",
    "updated": "2026-01-16T18:26:50Z",
    "expires": "2026-08-13T04:00:00Z",
    "registrar": {
      "name": "RESERVED-Internet Assigned Numbers Authority",
      "url": "https://www.iana.org"
    },
    "selfLink": "https://rdap.verisign.com/com/v1/domain/EXAMPLE.COM"
  }
}
```

| Field     | Type                    | Description                                                  |
|-----------|-------------------------|--------------------------------------------------------------|
| `status`  | `string`                | `SUCCESS`, `NOTFOUND`, `ERROR`, or `RATELIMIT`               |
| `answers` | `RDAPResult \| string`  | Parsed RDAP result on success, or an error message string.   |

**`RDAPResult` object:**

| Field              | Type        | Description                                               |
|--------------------|-------------|-----------------------------------------------------------|
| `query`            | `string`    | The original query value                                  |
| `queryType`        | `string`    | `domain`, `ipv4`, `ipv6`, or `asn`                        |
| `rdapServer`       | `string`    | Authoritative RDAP server URL used for this query         |
| `handle`           | `string`    | Registry object handle / identifier                       |
| `ldhName`          | `string`    | LDH domain name (domain lookups)                          |
| `unicodeName`      | `string`    | Internationalised domain name / IDN (domain lookups)      |
| `status`           | `string[]`  | RDAP status codes (e.g. `active`, `inactive`)             |
| `nameServers`      | `string[]`  | Authoritative name servers (domain lookups)               |
| `delegationSigned` | `boolean`   | DNSSEC delegation signing status (domain lookups)         |
| `dnsKeyData`       | `string`    | DNSSEC key data, if present (domain lookups)              |
| `startAddress`     | `string`    | First IP of the network block (IP lookups)                |
| `endAddress`       | `string`    | Last IP of the network block (IP lookups)                 |
| `ipVersion`        | `string`    | `v4` or `v6` (IP lookups)                                 |
| `cidrs`            | `string[]`  | CIDR notation(s) of the network block (IP lookups)        |
| `startAutnum`      | `integer`   | First AS number in range (ASN lookups)                    |
| `endAutnum`        | `integer`   | Last AS number in range (ASN lookups)                     |
| `name`             | `string`    | Network or AS name                                        |
| `type`             | `string`    | Allocation type or category                               |
| `country`          | `string`    | Two-letter country code                                   |
| `created`          | `date-time` | Registration / creation date                              |
| `updated`          | `date-time` | Last update date                                          |
| `expires`          | `date-time` | Expiry date (domain lookups)                              |
| `registrar`        | `Entity`    | Sponsoring registrar                                      |
| `registrant`       | `Entity`    | Registrant contact                                        |
| `admin`            | `Entity`    | Administrative contact                                    |
| `tech`             | `Entity`    | Technical contact                                         |
| `abuse`            | `Entity`    | Abuse contact                                             |
| `remarks`          | `string[]`  | Remarks and notices from the RDAP response                |
| `selfLink`         | `string`    | Canonical RDAP URL for this object                        |
| `rawData`          | `object`    | Raw parsed RDAP JSON response                             |

Each `Entity` object:

| Field          | Type       | Description              |
|----------------|------------|--------------------------|
| `handle`       | `string`   | Registry handle          |
| `roles`        | `string[]` | Entity roles             |
| `name`         | `string`   | Full name                |
| `organization` | `string`   | Organization             |
| `email`        | `string`   | Email address            |
| `phone`        | `string`   | Voice phone number       |
| `fax`          | `string`   | Fax number               |
| `street`       | `string`   | Street address           |
| `city`         | `string`   | City                     |
| `state`        | `string`   | State or province        |
| `postalCode`   | `string`   | Postal code              |
| `country`      | `string`   | Country code             |
| `url`          | `string`   | Website URL              |

#### Status codes

| Value       | HTTP | Meaning                                                                           |
|-------------|------|-----------------------------------------------------------------------------------|
| `SUCCESS`   | 200  | Query succeeded, `answers` contains the parsed RDAP result                        |
| `NOTFOUND`  | 200  | No RDAP record found for the target                                               |
| `ERROR`     | 200  | Query failed (bad request, network error, or invalid input)                       |
| `RATELIMIT` | 429  | The authoritative RDAP server returned HTTP 429 — back off and retry later        |

#### Examples

**Resolve a domain name:**

```bash
curl -s -X POST http://localhost:8080/api/v1/rdap \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com"}' | jq .
```

```json
{
  "status": "SUCCESS",
  "answers": {
    "query": "example.com",
    "queryType": "domain",
    "rdapServer": "https://rdap.verisign.com/com/v1/",
    "handle": "2336799_DOMAIN_COM-VRSN",
    "ldhName": "example.com",
    "status": ["active"],
    "nameServers": ["a.iana-servers.net", "b.iana-servers.net"],
    "delegationSigned": false,
    "created": "1995-08-14T04:00:00Z",
    "expires": "2026-08-13T04:00:00Z",
    "selfLink": "https://rdap.verisign.com/com/v1/domain/EXAMPLE.COM"
  }
}
```

**Look up an IP address:**

```bash
curl -s -X POST http://localhost:8080/api/v1/rdap \
  -H "Content-Type: application/json" \
  -d '{"ip":"8.8.8.8"}' | jq .
```

```json
{
  "status": "SUCCESS",
  "answers": {
    "query": "8.8.8.8",
    "queryType": "ip",
    "rdapServer": "https://rdap.arin.net/registry/",
    "handle": "NET-8-8-8-0-2",
    "startAddress": "8.8.8.0",
    "endAddress": "8.8.8.255",
    "ipVersion": "v4",
    "cidrs": ["8.8.8.0/24"],
    "name": "GOGL",
    "type": "DIRECT ALLOCATION",
    "country": "US"
  }
}
```

**Look up an AS number:**

```bash
curl -s -X POST http://localhost:8080/api/v1/rdap \
  -H "Content-Type: application/json" \
  -d '{"asn":"AS15169"}' | jq .
```

```json
{
  "status": "SUCCESS",
  "answers": {
    "query": "AS15169",
    "queryType": "asn",
    "rdapServer": "https://rdap.arin.net/registry/",
    "handle": "AS15169",
    "startAutnum": 15169,
    "endAutnum": 15169,
    "name": "GOOGLE"
  }
}
```

---

### `GET /`

Returns the embedded interactive web UI.

### `GET /openapi.json`

Returns the full OpenAPI 3.0 specification of the API.

### `GET /favicon.png`

Returns the application icon.

---

## Development

Dependencies are managed with Go modules and vendored in the `vendor/` directory. After cloning:

```bash
bash scripts/000_init.sh
go build ./...
```

The test suite and initialization scripts are located in `scripts/`:

```
scripts/000_init.sh     # go mod tidy && go mod vendor
scripts/999_test.sh     # Integration tests (server must be running)
```

---

## AI-Assisted Development

This project was developed with the assistance of **[Claude Sonnet 4.6](https://www.anthropic.com/claude)** by Anthropic.

Migrated from `http2whois` (WHOIS-over-HTTP) to `http2rdap` (RDAP-over-HTTP) by **[Claude Sonnet 4.6](https://www.anthropic.com/claude)** by Anthropic.

---

## See also

| Project | GitHub | Docker Hub | Description |
|---|---|---|---|
| `http2dns` | [letstool/http2dns](https://github.com/letstool/http2dns) | [letstool/http2dns](https://hub.docker.com/r/letstool/http2dns) | Fast & lightweight HTTP gateway that serves DNS queries as a JSON REST API |
| `http2whois` | [letstool/http2whois](https://github.com/letstool/http2whois) | [letstool/http2whois](https://hub.docker.com/r/letstool/http2whois) | Fast & lightweight HTTP gateway that serves WHOIS queries as a JSON REST API |
| `http2rdap` | [letstool/http2rdap](https://github.com/letstool/http2rdap) | [letstool/http2rdap](https://hub.docker.com/r/letstool/http2rdap) | Fast & lightweight HTTP gateway that serves RDAP queries as a JSON REST API |
| `http2geoip` | [letstool/http2geoip](https://github.com/letstool/http2geoip) | [letstool/http2geoip](https://hub.docker.com/r/letstool/http2geoip) | Fast & lightweight HTTP gateway that serves IP geolocation database as a JSON REST API |
| `http2cert` | [letstool/http2cert](https://github.com/letstool/http2cert) | [letstool/http2cert](https://hub.docker.com/r/letstool/http2cert) | Fast & lightweight HTTP gateway that serves X.509 certificate inspection as a JSON REST API |
| `http2tor` | [letstool/http2tor](https://github.com/letstool/http2tor) | [letstool/http2tor](https://hub.docker.com/r/letstool/http2tor) | Fast & lightweight HTTP gateway that serves Tor IP database as a JSON REST API |
| `http2sun` | [letstool/http2sun](https://github.com/letstool/http2sun) | [letstool/http2sun](https://hub.docker.com/r/letstool/http2sun) | Fast & lightweight HTTP gateway that serves solar position algorithm as a JSON REST API |
| `http2mac` | [letstool/http2mac](https://github.com/letstool/http2mac) | [letstool/http2mac](https://hub.docker.com/r/letstool/http2mac) | Fast & lightweight HTTP gateway that serves MAC address OUI database as a JSON REST API |
| `http2country` | [letstool/http2country](https://github.com/letstool/http2country) | [letstool/http2country](https://hub.docker.com/r/letstool/http2country) | Fast & lightweight HTTP gateway that serves country database as a JSON REST API |
| `http2prefix` | [letstool/http2prefix](https://github.com/letstool/http2prefix) | [letstool/http2prefix](https://hub.docker.com/r/letstool/http2prefix) | Fast & lightweight HTTP gateway that serves Internet BGP routing database as a JSON REST API |
