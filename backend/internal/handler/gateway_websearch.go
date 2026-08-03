package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
)

const (
	maxStandaloneSearchQueries = 4
	maxStandaloneOutputChars   = 64 * 1024
	maxStandaloneTitleChars    = 300
	maxStandaloneSnippetChars  = 2000
	maxStandaloneURLChars      = 2048
	maxStandaloneOutputTokens  = 1_000_000
)

type standaloneSearchRequest struct {
	ID              string                     `json:"id"`
	Model           string                     `json:"model"`
	Commands        map[string]json.RawMessage `json:"commands"`
	Settings        standaloneSearchSettings   `json:"settings"`
	MaxOutputTokens int                        `json:"max_output_tokens"`
}

type standaloneSearchSettings struct {
	ExternalWebAccess json.RawMessage `json:"external_web_access"`
}

type standaloneSearchQuery struct {
	Q       string   `json:"q"`
	Recency int      `json:"recency"`
	Domains []string `json:"domains"`
}

type standaloneTextResult struct {
	Type    string `json:"type"`
	RefID   string `json:"ref_id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
	PageAge string `json:"page_age,omitempty"`
}

type standaloneSearchResponse struct {
	EncryptedOutput *string                `json:"encrypted_output"`
	Output          string                 `json:"output"`
	Results         []standaloneTextResult `json:"results"`
}

func parseStandaloneSearchRequest(c *gin.Context) (*standaloneSearchRequest, []standaloneSearchQuery, int, error) {
	var req standaloneSearchRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&req); err != nil {
		return nil, nil, 0, fmt.Errorf("invalid JSON request")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, nil, 0, fmt.Errorf("invalid JSON request")
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Model) == "" {
		return nil, nil, 0, fmt.Errorf("id and model are required")
	}
	if req.MaxOutputTokens < 0 || req.MaxOutputTokens > maxStandaloneOutputTokens {
		return nil, nil, 0, fmt.Errorf("max_output_tokens must be between 0 and %d", maxStandaloneOutputTokens)
	}
	for command := range req.Commands {
		if command != "search_query" && command != "response_length" {
			return nil, nil, 0, fmt.Errorf("unsupported standalone search command %q", command)
		}
	}
	var queries []standaloneSearchQuery
	if raw := req.Commands["search_query"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &queries); err != nil {
			return nil, nil, 0, fmt.Errorf("invalid search_query command")
		}
	}
	if len(queries) == 0 || len(queries) > maxStandaloneSearchQueries {
		return nil, nil, 0, fmt.Errorf("search_query must contain 1 to %d queries", maxStandaloneSearchQueries)
	}
	maxResults := 5
	if raw := req.Commands["response_length"]; len(raw) > 0 {
		var responseLength string
		if err := json.Unmarshal(raw, &responseLength); err != nil {
			return nil, nil, 0, fmt.Errorf("invalid response_length")
		}
		switch responseLength {
		case "short":
			maxResults = 5
		case "medium":
			maxResults = 10
		case "long":
			maxResults = 20
		default:
			return nil, nil, 0, fmt.Errorf("unsupported response_length %q", responseLength)
		}
	}
	if err := validateStandaloneExternalWebAccess(req.Settings.ExternalWebAccess); err != nil {
		return nil, nil, 0, err
	}
	for index := range queries {
		queries[index].Q = strings.TrimSpace(queries[index].Q)
		for domainIndex := range queries[index].Domains {
			queries[index].Domains[domainIndex] = strings.ToLower(strings.TrimSpace(queries[index].Domains[domainIndex]))
		}
		if err := websearch.ValidateSearchRequest(websearch.SearchRequest{
			Query: queries[index].Q, MaxResults: maxResults, Domains: queries[index].Domains, RecencyDays: queries[index].Recency,
		}); err != nil {
			return nil, nil, 0, err
		}
	}
	return &req, queries, maxResults, nil
}

func validateStandaloneExternalWebAccess(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("invalid external_web_access setting")
	}
	if enabled, ok := value.(bool); ok && enabled {
		return nil
	}
	if mode, ok := value.(string); ok && mode == "live" {
		return nil
	}
	return fmt.Errorf("sub2api standalone search currently supports only live external web access")
}

func boundedStandaloneText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func normalizeStandaloneResult(result websearch.SearchResult, refID string) (standaloneTextResult, error) {
	if len(result.URL) == 0 || len(result.URL) > maxStandaloneURLChars {
		return standaloneTextResult{}, fmt.Errorf("provider returned an invalid URL")
	}
	parsed, err := url.Parse(result.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return standaloneTextResult{}, fmt.Errorf("provider returned an invalid URL")
	}
	return standaloneTextResult{
		Type: "text_result", RefID: refID, URL: result.URL,
		Title:   boundedStandaloneText(result.Title, maxStandaloneTitleChars),
		Snippet: boundedStandaloneText(result.Snippet, maxStandaloneSnippetChars),
		PageAge: boundedStandaloneText(result.PageAge, 200),
	}, nil
}

func appendStandaloneOutput(builder *strings.Builder, query string, results []standaloneTextResult) {
	if len(results) == 0 {
		fmt.Fprintf(builder, "No search results found for: %s\n", query)
		return
	}
	fmt.Fprintf(builder, "Search results for %q:\n\n", query)
	for index, result := range results {
		fmt.Fprintf(builder, "%d. **%s**\n   %s\n", index+1, result.Title, result.URL)
		if result.Snippet != "" {
			fmt.Fprintf(builder, "   %s\n", result.Snippet)
		}
		_, _ = builder.WriteString("\n")
	}
}

func boundStandaloneOutput(output string, maxOutputTokens int) string {
	limit := maxStandaloneOutputChars
	if maxOutputTokens > 0 && maxOutputTokens <= maxStandaloneOutputChars/4 {
		limit = maxOutputTokens * 4
	}
	runes := []rune(output)
	if len(runes) <= limit {
		return output
	}
	const suffix = "\n[Search output truncated]"
	suffixRunes := []rune(suffix)
	if limit <= len(suffixRunes) {
		return string(suffixRunes[:limit])
	}
	return string(runes[:limit-len(suffixRunes)]) + suffix
}

// WebSearch handles the OpenAI/Codex standalone-search compatibility endpoint.
func (h *GatewayHandler) WebSearch(c *gin.Context) {
	if h.webSearch == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Web search is temporarily unavailable")
		return
	}
	req, queries, maxResults, err := parseStandaloneSearchRequest(c)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	results := make([]standaloneTextResult, 0)
	var output strings.Builder
	for _, query := range queries {
		response, _, searchErr := h.webSearch(c.Request.Context(), websearch.SearchRequest{
			Query: query.Q, MaxResults: maxResults, Domains: query.Domains, RecencyDays: query.Recency,
		})
		if searchErr != nil || response == nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Web search is temporarily unavailable")
			return
		}
		queryResults := make([]standaloneTextResult, 0, len(response.Results))
		for _, result := range response.Results {
			if len(queryResults) >= maxResults {
				break
			}
			normalized, normalizeErr := normalizeStandaloneResult(result, fmt.Sprintf("turn0search%d", len(results)))
			if normalizeErr != nil {
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Web search is temporarily unavailable")
				return
			}
			results = append(results, normalized)
			queryResults = append(queryResults, normalized)
		}
		appendStandaloneOutput(&output, query.Q, queryResults)
	}
	formattedOutput := strings.TrimRight(output.String(), "\n") + "\n"
	c.JSON(http.StatusOK, standaloneSearchResponse{
		EncryptedOutput: nil,
		Output:          boundStandaloneOutput(formattedOutput, req.MaxOutputTokens),
		Results:         results,
	})
}
