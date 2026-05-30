package main

import (
	_ "embed"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/breml/rootcerts" // embed Mozilla root CAs for TLS in scratch images
	"nettools/http2rdap/internal/rdapclient"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/favicon.png
var faviconPNG []byte

//go:embed static/openapi.json
var openapiJSON []byte

/* ---------- Configuration ---------- */

// config holds all server-level settings resolved at startup.
type config struct {
	addr           string        // listening address (host:port)
	rdapTimeout    time.Duration // per-query RDAP HTTP timeout
	requestTimeout time.Duration // global HTTP request deadline
	tempDir        string        // directory for IANA bootstrap file cache
}

// resolveConfig builds the effective configuration by applying the priority rule:
//
//	CLI flag (if explicitly set) > environment variable > built-in default
func resolveConfig() config {
	const (
		defaultAddr           = "127.0.0.1:8080"
		defaultRDAPTimeout    = 15 * time.Second
		defaultRequestTimeout = 20 * time.Second
	)

	flagAddr := flag.String(
		"addr", "",
		"listening address (host:port)  [env: LISTEN_ADDR, default: "+defaultAddr+"]",
	)
	flagRDAPTimeout := flag.Duration(
		"rdap-timeout", 0,
		"per-query RDAP HTTP timeout  [env: RDAP_TIMEOUT, default: 15s]",
	)
	flagRequestTimeout := flag.Duration(
		"request-timeout", 0,
		"global HTTP request deadline    [env: REQUEST_TIMEOUT, default: 20s]",
	)
	flagTempDir := flag.String(
		"temp-dir", "",
		"directory for IANA bootstrap cache  [env: TEMP_DIR, default: system temp]",
	)
	flag.Parse()

	cfg := config{}

	cfg.addr = resolve(*flagAddr, os.Getenv("LISTEN_ADDR"), defaultAddr)
	cfg.tempDir = resolve(*flagTempDir, os.Getenv("TEMP_DIR"), "")

	if rt, err := parseDuration(*flagRDAPTimeout, "RDAP_TIMEOUT"); err == nil {
		cfg.rdapTimeout = rt
	} else {
		cfg.rdapTimeout = defaultRDAPTimeout
	}

	if rt, err := parseDuration(*flagRequestTimeout, "REQUEST_TIMEOUT"); err == nil {
		cfg.requestTimeout = rt
	} else {
		cfg.requestTimeout = defaultRequestTimeout
	}

	return cfg
}

func resolve(flagVal, envVal, fallback string) string {
	if flagVal != "" {
		return flagVal
	}
	if envVal != "" {
		return envVal
	}
	return fallback
}

func parseDuration(flagVal time.Duration, envKey string) (time.Duration, error) {
	if flagVal != 0 {
		return flagVal, nil
	}
	if raw := os.Getenv(envKey); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid %s=%q: %w", envKey, raw, err)
		}
		return d, nil
	}
	return 0, fmt.Errorf("not set")
}

/* ---------- Request / response structures ---------- */

// RDAPRequest is the JSON body accepted by POST /api/v1/rdap.
// Exactly one of ip, domain, or asn must be provided.
type RDAPRequest struct {
	IP      string `json:"ip"`
	Domain  string `json:"domain"`
	ASN     string `json:"asn"`
	Timeout int    `json:"timeout"` // seconds (0 -> use server default)
}

// RDAPResponse is the JSON envelope returned for every request.
type RDAPResponse struct {
	Status  string      `json:"status"`
	Answers interface{} `json:"answers"`
}

/* ---------- HTTP handlers ---------- */

func indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Write(faviconPNG)
}

func openapiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiJSON)
}

// makeRDAPHandler returns an http.HandlerFunc closed over the server config.
func makeRDAPHandler(defaultClient *rdapclient.Client, cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req RDAPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, RDAPResponse{Status: "ERROR", Answers: "invalid JSON"})
			return
		}
		defer r.Body.Close()

		// Validate and build the query target.
		var query string
		switch {
		case req.IP != "":
			query = req.IP
		case req.Domain != "":
			query = req.Domain
		case req.ASN != "":
			asn := req.ASN
			// Normalise: strip leading "AS" so both "15169" and "AS15169" work.
			if strings.HasPrefix(strings.ToUpper(asn), "AS") {
				asn = asn[2:]
			}
			query = asn
		default:
			respondJSON(w, RDAPResponse{Status: "ERROR", Answers: "neither ip, domain nor asn provided"})
			return
		}

		// Use a per-request client when a custom timeout is requested.
		client := defaultClient
		if req.Timeout > 0 {
			client = rdapclient.New(cfg.tempDir,
				rdapclient.WithTimeout(time.Duration(req.Timeout)*time.Second))
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.requestTimeout)
		defer cancel()

		res, err := client.Lookup(ctx, query)
		if err != nil {
			if rdapclient.IsRateLimit(err) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(RDAPResponse{Status: "RATELIMIT", Answers: err.Error()}) //nolint:errcheck
				return
			}
			respondJSON(w, RDAPResponse{Status: "ERROR", Answers: err.Error()})
			return
		}
		if res.RawData == nil {
			respondJSON(w, RDAPResponse{Status: "NOTFOUND", Answers: []string{}})
			return
		}

		respondJSON(w, RDAPResponse{Status: "SUCCESS", Answers: res})
	}
}

/* ---------- Helpers ---------- */

func respondJSON(w http.ResponseWriter, resp RDAPResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

/* ---------- Entry point ---------- */

func main() {
	cfg := resolveConfig()

	defaultClient := rdapclient.New(cfg.tempDir,
		rdapclient.WithTimeout(cfg.rdapTimeout),
	)

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/favicon.png", faviconHandler)
	http.HandleFunc("/openapi.json", openapiHandler)
	http.HandleFunc("/api/v1/rdap", makeRDAPHandler(defaultClient, cfg))

	tempDirDisplay := cfg.tempDir
	if tempDirDisplay == "" {
		tempDirDisplay = "(system default)"
	}
	fmt.Printf("RDAP API listening on %s (rdap-timeout=%s, request-timeout=%s, temp-dir=%s)\n",
		cfg.addr, cfg.rdapTimeout, cfg.requestTimeout, tempDirDisplay)

	if err := http.ListenAndServe(cfg.addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "Server error:", err)
		os.Exit(1)
	}
}
