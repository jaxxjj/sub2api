package websearch

import (
	"fmt"
	"strings"
)

// SearchResult represents a single web search result.
type SearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	PageAge string `json:"page_age,omitempty"`
}

// SearchRequest describes a web search to perform.
type SearchRequest struct {
	Query       string
	MaxResults  int    // defaults to defaultMaxResults if <= 0
	ProxyURL    string // optional HTTP proxy URL
	Domains     []string
	RecencyDays int
}

// SearchResponse holds the results of a web search.
type SearchResponse struct {
	Results []SearchResult
	Query   string // the query that was actually executed
}

const defaultMaxResults = 5

const (
	maxQueryLength   = 2048
	maxDomainCount   = 10
	maxDomainLength  = 253
	maxRecencyDays   = 3650
	maxSearchResults = 20
)

// ValidateSearchRequest checks the provider-neutral search boundary.
func ValidateSearchRequest(req SearchRequest) error {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return fmt.Errorf("websearch: empty search query")
	}
	if len(query) > maxQueryLength {
		return fmt.Errorf("websearch: query must contain 1 to %d characters", maxQueryLength)
	}
	if req.MaxResults < 0 || req.MaxResults > maxSearchResults {
		return fmt.Errorf("websearch: max results must be 0 (default) or between 1 and %d", maxSearchResults)
	}
	if req.RecencyDays < 0 || req.RecencyDays > maxRecencyDays {
		return fmt.Errorf("websearch: recency must be 0 (any time) or between 1 and %d days", maxRecencyDays)
	}
	if len(req.Domains) > maxDomainCount {
		return fmt.Errorf("websearch: too many domains")
	}
	for _, domain := range req.Domains {
		if !validSearchDomain(domain) {
			return fmt.Errorf("websearch: invalid domain %q", domain)
		}
	}
	return nil
}

func validSearchDomain(raw string) bool {
	domain := strings.TrimSpace(strings.ToLower(raw))
	if domain == "" || len(domain) > maxDomainLength || domain != raw || strings.ContainsAny(domain, "/:") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

// Provider type identifiers.
const (
	ProviderTypeBrave  = "brave"
	ProviderTypeTavily = "tavily"
)
