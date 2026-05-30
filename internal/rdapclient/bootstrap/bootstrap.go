// Package bootstrap resolves the authoritative RDAP server URL for a query
// by consulting the IANA RDAP bootstrap registry files defined in RFC 9224.
//
// Bootstrap files are downloaded once and cached on disk under tempDir with
// a 24-hour TTL. If a cached file is still fresh it is read from disk,
// otherwise it is re-fetched from IANA and the cache is updated.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ianaBase = "https://data.iana.org/rdap/"
	cacheTTL = 24 * time.Hour

	urlDNS  = ianaBase + "dns.json"
	urlIPv4 = ianaBase + "ipv4.json"
	urlIPv6 = ianaBase + "ipv6.json"
	urlASN  = ianaBase + "asn.json"
)

// bootstrapFile mirrors the IANA bootstrap JSON format (RFC 9224 §3).
// Each element of Services is a two-element slice: [keys, serverURLs].
type bootstrapFile struct {
	Services [][][]string `json:"services"`
}

// Resolver fetches, caches, and queries IANA RDAP bootstrap files.
type Resolver struct {
	tempDir string
	client  *http.Client
	mu      sync.Mutex
	cache   map[string]*cachedFile
}

type cachedFile struct {
	data      *bootstrapFile
	fetchedAt time.Time
}

// New returns a Resolver that stores bootstrap cache files under tempDir.
// If tempDir is empty, os.TempDir() is used.
func New(tempDir string, httpClient *http.Client) *Resolver {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Resolver{
		tempDir: tempDir,
		client:  httpClient,
		cache:   make(map[string]*cachedFile),
	}
}

// ForDomain returns the base RDAP server URL for the given domain name.
// It tries the longest TLD suffix first (e.g. "co.uk" before "uk").
func (r *Resolver) ForDomain(domain string) (string, error) {
	bf, err := r.get("dns", urlDNS)
	if err != nil {
		return "", fmt.Errorf("bootstrap DNS: %w", err)
	}

	labels := strings.Split(strings.ToLower(strings.TrimSuffix(domain, ".")), ".")
	// Try from most-specific suffix down to the TLD alone.
	for i := len(labels) - 2; i >= 0; i-- {
		suffix := strings.Join(labels[i:], ".")
		for _, svc := range bf.Services {
			if len(svc) < 2 {
				continue
			}
			for _, key := range svc[0] {
				if strings.EqualFold(key, suffix) && len(svc[1]) > 0 {
					return strings.TrimSuffix(svc[1][0], "/"), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no RDAP server found for domain %q", domain)
}

// ForIP returns the base RDAP server URL for the given IP address (v4 or v6).
// When multiple CIDRs match the address the most-specific prefix wins.
func (r *Resolver) ForIP(ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("invalid IP address: %s", ip)
	}

	var bfURL, bfName string
	if parsed.To4() != nil {
		bfURL, bfName = urlIPv4, "ipv4"
	} else {
		bfURL, bfName = urlIPv6, "ipv6"
	}

	bf, err := r.get(bfName, bfURL)
	if err != nil {
		return "", fmt.Errorf("bootstrap %s: %w", bfName, err)
	}

	// Find the service whose CIDR best contains the IP (longest prefix wins).
	bestLen := -1
	bestURL := ""
	for _, svc := range bf.Services {
		if len(svc) < 2 || len(svc[1]) == 0 {
			continue
		}
		for _, cidr := range svc[0] {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if !network.Contains(parsed) {
				continue
			}
			plen, _ := network.Mask.Size()
			if plen > bestLen {
				bestLen = plen
				bestURL = strings.TrimSuffix(svc[1][0], "/")
			}
		}
	}
	if bestURL == "" {
		return "", fmt.Errorf("no RDAP server found for IP %q", ip)
	}
	return bestURL, nil
}

// ForASN returns the base RDAP server URL for the given AS number.
func (r *Resolver) ForASN(asn int64) (string, error) {
	bf, err := r.get("asn", urlASN)
	if err != nil {
		return "", fmt.Errorf("bootstrap ASN: %w", err)
	}

	for _, svc := range bf.Services {
		if len(svc) < 2 || len(svc[1]) == 0 {
			continue
		}
		for _, entry := range svc[0] {
			parts := strings.SplitN(entry, "-", 2)
			start, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			if err1 != nil {
				continue
			}
			end := start
			if len(parts) == 2 {
				end, _ = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			}
			if asn >= start && asn <= end {
				return strings.TrimSuffix(svc[1][0], "/"), nil
			}
		}
	}
	return "", fmt.Errorf("no RDAP server found for ASN %d", asn)
}

// get returns a bootstrapFile for the given name/url, using the in-memory
// cache first, then the on-disk cache, then fetching from the network.
func (r *Resolver) get(name, url string) (*bootstrapFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// In-memory cache hit (still fresh)?
	if c, ok := r.cache[name]; ok && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	// On-disk cache hit?
	diskPath := filepath.Join(r.tempDir, "rdap_bootstrap_"+name+".json")
	if info, err := os.Stat(diskPath); err == nil && time.Since(info.ModTime()) < cacheTTL {
		if bf, err := readDiskCache(diskPath); err == nil {
			r.cache[name] = &cachedFile{data: bf, fetchedAt: info.ModTime()}
			return bf, nil
		}
	}

	// Fetch from IANA.
	bf, err := r.fetch(url)
	if err != nil {
		// If fetch fails but we have a stale disk cache, use it rather than fail.
		if bf2, err2 := readDiskCache(diskPath); err2 == nil {
			return bf2, nil
		}
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	// Persist to disk (best-effort).
	if raw, err := json.Marshal(bf); err == nil {
		_ = os.MkdirAll(r.tempDir, 0o755)
		_ = os.WriteFile(diskPath, raw, 0o644)
	}

	r.cache[name] = &cachedFile{data: bf, fetchedAt: time.Now()}
	return bf, nil
}

func (r *Resolver) fetch(url string) (*bootstrapFile, error) {
	resp, err := r.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	var bf bootstrapFile
	if err := json.NewDecoder(resp.Body).Decode(&bf); err != nil {
		return nil, err
	}
	return &bf, nil
}

func readDiskCache(path string) (*bootstrapFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bf bootstrapFile
	if err := json.Unmarshal(raw, &bf); err != nil {
		return nil, err
	}
	return &bf, nil
}
