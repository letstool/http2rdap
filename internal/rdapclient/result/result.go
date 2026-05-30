// Package result defines the structured output of an RDAP lookup.
package result

// Entity holds contact / organization information extracted from an RDAP entity object.
type Entity struct {
	Handle       string   `json:"handle,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Email        string   `json:"email,omitempty"`
	Phone        string   `json:"phone,omitempty"`
	Fax          string   `json:"fax,omitempty"`
	Street       string   `json:"street,omitempty"`
	City         string   `json:"city,omitempty"`
	State        string   `json:"state,omitempty"`
	PostalCode   string   `json:"postalCode,omitempty"`
	Country      string   `json:"country,omitempty"`
	URL          string   `json:"url,omitempty"`
}

// Result is the structured output of an RDAP lookup.
type Result struct {
	// Query metadata
	Query      string `json:"query"`
	QueryType  string `json:"queryType"` // "domain", "ip", "asn"
	RDAPServer string `json:"rdapServer,omitempty"`

	// Domain fields
	Handle      string   `json:"handle,omitempty"`
	LdhName     string   `json:"ldhName,omitempty"`
	UnicodeName string   `json:"unicodeName,omitempty"`
	Status      []string `json:"status,omitempty"`
	NameServers []string `json:"nameServers,omitempty"`
	// DNSSEC
	DelegationSigned *bool  `json:"delegationSigned,omitempty"`
	DNSKeyData       string `json:"dnsKeyData,omitempty"`

	// IP network fields
	StartAddress string   `json:"startAddress,omitempty"`
	EndAddress   string   `json:"endAddress,omitempty"`
	IPVersion    string   `json:"ipVersion,omitempty"`
	CIDRs        []string `json:"cidrs,omitempty"`

	// ASN fields
	StartAutnum int64 `json:"startAutnum,omitempty"`
	EndAutnum   int64 `json:"endAutnum,omitempty"`

	// Common (name/type used by IP networks, ASNs, and some domain registrars)
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Country string `json:"country,omitempty"`

	// Dates extracted from RDAP events
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
	Expires string `json:"expires,omitempty"`

	// Well-known entity roles
	Registrar  *Entity `json:"registrar,omitempty"`
	Registrant *Entity `json:"registrant,omitempty"`
	Admin      *Entity `json:"admin,omitempty"`
	Tech       *Entity `json:"tech,omitempty"`
	Abuse      *Entity `json:"abuse,omitempty"`

	// Remarks/notices (human-readable notes from the registry)
	Remarks []string `json:"remarks,omitempty"`

	// Self link from the RDAP response
	SelfLink string `json:"selfLink,omitempty"`

	// Raw RDAP response as returned by the authoritative server
	RawData interface{} `json:"rawData,omitempty"`
}
