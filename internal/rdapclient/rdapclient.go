// Package rdapclient provides RDAP lookups for domain names, IPv4/IPv6
// addresses, and AS numbers.  It resolves the authoritative RDAP server via
// the IANA bootstrap registry (RFC 9224) and returns structured data parsed
// from the RDAP JSON response (RFC 9083).
package rdapclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"nettools/http2rdap/internal/rdapclient/bootstrap"
	"nettools/http2rdap/internal/rdapclient/result"
)

// ErrRateLimit is returned when the authoritative RDAP server replies with
// HTTP 429 Too Many Requests.  Callers can detect it with errors.As:
//
//	var e *ErrRateLimit
//	if errors.As(err, &e) { /* handle rate-limit */ }
type ErrRateLimit struct {
	// Server is the RDAP server URL that rejected the request.
	Server string
}

func (e *ErrRateLimit) Error() string {
	return fmt.Sprintf("RDAP server rate limit exceeded (HTTP 429): %s", e.Server)
}

// IsRateLimit reports whether err (or any error in its chain) is an
// ErrRateLimit.  Convenience wrapper around errors.As.
func IsRateLimit(err error) bool {
	var e *ErrRateLimit
	return errors.As(err, &e)
}

// Client performs RDAP lookups.
type Client struct {
	bootstrap  *bootstrap.Resolver
	httpClient *http.Client
}

// Option is a functional option for Client.
type Option func(*Client)

// WithTimeout sets the per-request HTTP timeout (default 15 s).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// New creates a Client using the given options.
// tempDir specifies where bootstrap files are cached; pass "" for os.TempDir().
func New(tempDir string, opts ...Option) *Client {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	c := &Client{
		bootstrap:  bootstrap.New(tempDir, &http.Client{Timeout: 15 * time.Second}),
		httpClient: httpClient,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Lookup performs an RDAP lookup for the given target.
// The target may be:
//   - a domain name         ("example.com")
//   - an IPv4 address       ("8.8.8.8")
//   - an IPv6 address       ("2001:4860:4860::8888")
//   - an AS number          ("AS15169", "15169")
func (c *Client) Lookup(ctx context.Context, target string) (*result.Result, error) {
	target = strings.TrimSpace(target)
	qtype, serverURL, rdapPath, asnNum, err := c.resolve(ctx, target)
	if err != nil {
		return nil, err
	}

	url := serverURL + rdapPath
	raw, httpStatus, err := c.fetch(ctx, url)
	if err != nil {
		return nil, err
	}

	res := &result.Result{
		Query:      target,
		QueryType:  qtype,
		RDAPServer: serverURL,
	}

	if httpStatus == http.StatusNotFound {
		return res, nil // caller checks for empty RawData
	}
	if httpStatus == http.StatusTooManyRequests {
		return nil, &ErrRateLimit{Server: serverURL}
	}
	if httpStatus != http.StatusOK {
		return nil, fmt.Errorf("RDAP server returned HTTP %d for %s", httpStatus, url)
	}

	// Store the raw RDAP response.
	var rawObj interface{}
	if err := json.Unmarshal(raw, &rawObj); err == nil {
		res.RawData = rawObj
	}

	// Parse into the structured result.
	var rdap map[string]interface{}
	if err := json.Unmarshal(raw, &rdap); err != nil {
		return nil, fmt.Errorf("parse RDAP response: %w", err)
	}
	parseRDAP(rdap, res, qtype, asnNum)

	return res, nil
}

// resolve determines the query type, the bootstrap-resolved base server URL,
// the RDAP path, and (for ASNs) the numeric ASN value.
func (c *Client) resolve(ctx context.Context, target string) (qtype, serverURL, rdapPath string, asnNum int64, err error) {
	upper := strings.ToUpper(target)

	// ASN?
	var rawASN string
	if strings.HasPrefix(upper, "AS") {
		rawASN = target[2:]
	} else if isAllDigits(target) {
		rawASN = target
	}
	if rawASN != "" {
		var n int64
		_, scanErr := fmt.Sscan(rawASN, &n)
		if scanErr == nil && n > 0 {
			serverURL, err = c.bootstrap.ForASN(n)
			if err != nil {
				return
			}
			return "asn", serverURL, fmt.Sprintf("/autnum/%d", n), n, nil
		}
	}

	// IPv4 / IPv6?
	if ip := net.ParseIP(target); ip != nil {
		serverURL, err = c.bootstrap.ForIP(target)
		if err != nil {
			return
		}
		if ip.To4() != nil {
			qtype = "ip"
		} else {
			qtype = "ip6"
		}
		return qtype, serverURL, "/ip/" + target, 0, nil
	}

	// Domain.
	serverURL, err = c.bootstrap.ForDomain(target)
	if err != nil {
		return
	}
	return "domain", serverURL, "/domain/" + strings.ToLower(target), 0, nil
}

// fetch performs a GET request and returns the response body and status code.
func (c *Client) fetch(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", "http2rdap/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB limit
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// parseRDAP extracts well-known fields from the raw RDAP map into res.
func parseRDAP(rdap map[string]interface{}, res *result.Result, qtype string, asnNum int64) {
	res.Handle = strField(rdap, "handle")
	res.Name = strField(rdap, "name")
	res.Type = strField(rdap, "type")
	res.Country = strField(rdap, "country")

	// Self link
	if links, ok := rdap["links"].([]interface{}); ok {
		for _, l := range links {
			lm, ok := l.(map[string]interface{})
			if !ok {
				continue
			}
			if strField(lm, "rel") == "self" {
				res.SelfLink = strField(lm, "href")
				break
			}
		}
	}

	// Events -> dates
	if events, ok := rdap["events"].([]interface{}); ok {
		for _, ev := range events {
			evm, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}
			action := strField(evm, "eventAction")
			date := strField(evm, "eventDate")
			switch action {
			case "registration":
				res.Created = date
			case "last changed":
				res.Updated = date
			case "expiration":
				res.Expires = date
			}
		}
	}

	// Status
	if ss, ok := rdap["status"].([]interface{}); ok {
		for _, s := range ss {
			if str, ok := s.(string); ok {
				res.Status = append(res.Status, str)
			}
		}
	}

	// Remarks
	if remarks, ok := rdap["remarks"].([]interface{}); ok {
		for _, rem := range remarks {
			rm, ok := rem.(map[string]interface{})
			if !ok {
				continue
			}
			if desc, ok := rm["description"].([]interface{}); ok {
				for _, d := range desc {
					if str, ok := d.(string); ok && str != "" {
						res.Remarks = append(res.Remarks, str)
					}
				}
			}
		}
	}

	// Notices (treated the same as remarks for display)
	if notices, ok := rdap["notices"].([]interface{}); ok {
		for _, n := range notices {
			nm, ok := n.(map[string]interface{})
			if !ok {
				continue
			}
			if desc, ok := nm["description"].([]interface{}); ok {
				for _, d := range desc {
					if str, ok := d.(string); ok && str != "" && !isBoilerplate(str) {
						res.Remarks = append(res.Remarks, str)
					}
				}
			}
		}
	}

	switch qtype {
	case "domain":
		parseDomain(rdap, res)
	case "ip", "ip6":
		parseIPNetwork(rdap, res)
	case "asn":
		parseAutnum(rdap, res, asnNum)
	}

	// Entities (shared across all object classes)
	if entities, ok := rdap["entities"].([]interface{}); ok {
		parseEntities(entities, res)
	}
}

// parseDomain populates domain-specific fields.
func parseDomain(rdap map[string]interface{}, res *result.Result) {
	res.LdhName = strField(rdap, "ldhName")
	res.UnicodeName = strField(rdap, "unicodeName")

	// Name servers
	if ns, ok := rdap["nameservers"].([]interface{}); ok {
		for _, n := range ns {
			nm, ok := n.(map[string]interface{})
			if !ok {
				continue
			}
			if name := strField(nm, "ldhName"); name != "" {
				res.NameServers = append(res.NameServers, strings.ToLower(name))
			}
		}
	}

	// Secure DNS (DNSSEC)
	if sdns, ok := rdap["secureDNS"].(map[string]interface{}); ok {
		if del, ok := sdns["delegationSigned"].(bool); ok {
			b := del
			res.DelegationSigned = &b
		}
	}
}

// parseIPNetwork populates IP-network-specific fields.
func parseIPNetwork(rdap map[string]interface{}, res *result.Result) {
	res.StartAddress = strField(rdap, "startAddress")
	res.EndAddress = strField(rdap, "endAddress")
	res.IPVersion = strField(rdap, "ipVersion")

	// CIDRs from cidr0_cidrs extension (arin uses this)
	if cidrs, ok := rdap["cidr0_cidrs"].([]interface{}); ok {
		for _, c := range cidrs {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			v4prefix := strField(cm, "v4prefix")
			v6prefix := strField(cm, "v6prefix")
			length := ""
			if l, ok := cm["length"].(float64); ok {
				length = fmt.Sprintf("%d", int(l))
			}
			if v4prefix != "" && length != "" {
				res.CIDRs = append(res.CIDRs, v4prefix+"/"+length)
			} else if v6prefix != "" && length != "" {
				res.CIDRs = append(res.CIDRs, v6prefix+"/"+length)
			}
		}
	}
}

// parseAutnum populates ASN-specific fields.
func parseAutnum(rdap map[string]interface{}, res *result.Result, asnNum int64) {
	if v, ok := rdap["startAutnum"].(float64); ok {
		res.StartAutnum = int64(v)
	}
	if v, ok := rdap["endAutnum"].(float64); ok {
		res.EndAutnum = int64(v)
	}
	_ = asnNum
}

// parseEntities extracts well-known roles from the entity array.
func parseEntities(entities []interface{}, res *result.Result) {
	for _, e := range entities {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		roles := strSlice(em, "roles")
		ent := buildEntity(em)

		// Also recurse into nested entities for abuse contact etc.
		if nested, ok := em["entities"].([]interface{}); ok {
			parseEntities(nested, res)
		}

		for _, role := range roles {
			switch strings.ToLower(role) {
			case "registrar":
				if res.Registrar == nil {
					res.Registrar = ent
				}
			case "registrant":
				if res.Registrant == nil {
					res.Registrant = ent
				}
			case "administrative":
				if res.Admin == nil {
					res.Admin = ent
				}
			case "technical":
				if res.Tech == nil {
					res.Tech = ent
				}
			case "abuse":
				if res.Abuse == nil {
					res.Abuse = ent
				}
			}
		}
	}
}

// buildEntity constructs an Entity from an RDAP entity map.
func buildEntity(em map[string]interface{}) *result.Entity {
	ent := &result.Entity{
		Handle: strField(em, "handle"),
		Roles:  strSlice(em, "roles"),
	}

	// vCard
	if vc, ok := em["vcardArray"]; ok {
		parseVCard(vc, ent)
	}

	// URL from links
	if links, ok := em["links"].([]interface{}); ok {
		for _, l := range links {
			lm, ok := l.(map[string]interface{})
			if !ok {
				continue
			}
			if strField(lm, "rel") == "self" {
				ent.URL = strField(lm, "href")
				break
			}
		}
	}

	return ent
}

// parseVCard extracts contact fields from an RFC 6350 vCard embedded in RDAP.
// The RDAP vCard format is: ["vcard", [[propName, params, valueType, value], ...]]
func parseVCard(vcardArray interface{}, ent *result.Entity) {
	arr, ok := vcardArray.([]interface{})
	if !ok || len(arr) < 2 {
		return
	}
	props, ok := arr[1].([]interface{})
	if !ok {
		return
	}
	for _, p := range props {
		pa, ok := p.([]interface{})
		if !ok || len(pa) < 4 {
			continue
		}
		name, ok := pa[0].(string)
		if !ok {
			continue
		}
		val := pa[3]

		switch strings.ToLower(name) {
		case "fn":
			if ent.Name == "" {
				ent.Name = fmtVCardVal(val)
			}
		case "org":
			if ent.Organization == "" {
				ent.Organization = fmtVCardVal(val)
			}
		case "email":
			if ent.Email == "" {
				ent.Email = fmtVCardVal(val)
			}
		case "tel":
			// params contains type: ["voice"] or ["fax"]
			isFax := false
			if params, ok := pa[1].(map[string]interface{}); ok {
				if typeVal, ok := params["type"]; ok {
					typeStr := fmt.Sprintf("%v", typeVal)
					if strings.Contains(strings.ToLower(typeStr), "fax") {
						isFax = true
					}
				}
			}
			tel := fmtVCardVal(val)
			// Strip "tel:" URI prefix
			tel = strings.TrimPrefix(tel, "tel:")
			if isFax {
				if ent.Fax == "" {
					ent.Fax = tel
				}
			} else {
				if ent.Phone == "" {
					ent.Phone = tel
				}
			}
		case "adr":
			// Structured address: ["", poBox, street, city, state, postal, country]
			if parts, ok := val.([]interface{}); ok && len(parts) >= 7 {
				ent.Street = fmtVCardVal(parts[2])
				ent.City = fmtVCardVal(parts[3])
				ent.State = fmtVCardVal(parts[4])
				ent.PostalCode = fmtVCardVal(parts[5])
				ent.Country = fmtVCardVal(parts[6])
			}
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func strField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func strSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func fmtVCardVal(v interface{}) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []interface{}:
		parts := make([]string, 0, len(s))
		for _, p := range s {
			if str, ok := p.(string); ok && str != "" {
				parts = append(parts, str)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isBoilerplate returns true for well-known RDAP notice boilerplate strings
// that add no value to the user (terms of service sentences, etc.).
func isBoilerplate(s string) bool {
	lower := strings.ToLower(s)
	for _, kw := range []string{
		"terms of use", "terms of service", "subject to",
		"this output has been filtered", "please register",
		"copyright notice", "not authorized", "access to",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
