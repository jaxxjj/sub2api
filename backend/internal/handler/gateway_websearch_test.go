package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func runStandaloneSearchHandler(t *testing.T, h *GatewayHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.WebSearch(c)
	return recorder
}

func TestGatewayHandlerWebSearchReturnsOpenAICompatibleResponse(t *testing.T) {
	requests := make([]websearch.SearchRequest, 0, 2)
	h := &GatewayHandler{
		webSearch: func(_ context.Context, req websearch.SearchRequest) (*websearch.SearchResponse, string, error) {
			requests = append(requests, req)
			return &websearch.SearchResponse{
				Query: req.Query,
				Results: []websearch.SearchResult{{
					URL: "https://example.com/result", Title: "Result", Snippet: "Snippet", PageAge: "1 day ago",
				}},
			}, "brave", nil
		},
	}
	recorder := runStandaloneSearchHandler(t, h, `{
		"id":"search-session",
		"model":"gpt-search",
		"commands":{
			"search_query":[
				{"q":" first ","domains":[" EXAMPLE.COM "],"recency":7},
				{"q":"second"}
			],
			"response_length":"medium"
		},
		"settings":{"allowed_callers":["direct"],"external_web_access":true},
		"max_output_tokens":4000
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []websearch.SearchRequest{
		{Query: "first", Domains: []string{"example.com"}, RecencyDays: 7, MaxResults: 10},
		{Query: "second", MaxResults: 10},
	}, requests)
	require.JSONEq(t, `{
		"encrypted_output":null,
		"output":"Search results for \"first\":\n\n1. **Result**\n   https://example.com/result\n   Snippet\n\nSearch results for \"second\":\n\n1. **Result**\n   https://example.com/result\n   Snippet\n",
		"results":[
			{"type":"text_result","ref_id":"turn0search0","url":"https://example.com/result","title":"Result","snippet":"Snippet","page_age":"1 day ago"},
			{"type":"text_result","ref_id":"turn0search1","url":"https://example.com/result","title":"Result","snippet":"Snippet","page_age":"1 day ago"}
		]
	}`, recorder.Body.String())
}

func TestGatewayHandlerWebSearchRejectsUnsupportedCommandsAndModes(t *testing.T) {
	h := &GatewayHandler{webSearch: func(_ context.Context, _ websearch.SearchRequest) (*websearch.SearchResponse, string, error) {
		t.Fatal("search must not run")
		return nil, "", nil
	}}

	for _, body := range []string{
		`{"id":"id","model":"model","commands":{"open":[{"ref_id":"turn0search0"}]}}`,
		`{"id":"id","model":"model","commands":{"search_query":[{"q":"query"}]},"settings":{"external_web_access":"indexed"}}`,
		`{"id":"id","model":"model","commands":{"search_query":[{"q":"query","domains":["https://example.com"]}]}}`,
		`{"id":"id","model":"model","commands":{"search_query":[{"q":"query"}]},"max_output_tokens":1000001}`,
		`{"id":"id","model":"model","commands":{"search_query":[{"q":"query"}]}} {}`,
	} {
		recorder := runStandaloneSearchHandler(t, h, body)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "body=%s response=%s", body, recorder.Body.String())
	}
}

func TestGatewayHandlerWebSearchReturnsSafeServiceError(t *testing.T) {
	h := &GatewayHandler{webSearch: func(_ context.Context, _ websearch.SearchRequest) (*websearch.SearchResponse, string, error) {
		return nil, "", errors.New("provider leaked secret")
	}}
	recorder := runStandaloneSearchHandler(t, h, `{
		"id":"id","model":"model","commands":{"search_query":[{"q":"query"}]}
	}`)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "provider leaked secret")
	require.Contains(t, recorder.Body.String(), "Web search is temporarily unavailable")
}

func TestGatewayHandlerWebSearchBoundsProviderResults(t *testing.T) {
	providerResults := make([]websearch.SearchResult, 0, 30)
	for index := range 30 {
		providerResults = append(providerResults, websearch.SearchResult{
			URL: fmt.Sprintf("https://example.com/result/%d", index), Title: fmt.Sprintf("Result %d", index),
		})
	}
	h := &GatewayHandler{webSearch: func(_ context.Context, req websearch.SearchRequest) (*websearch.SearchResponse, string, error) {
		require.Equal(t, 5, req.MaxResults)
		return &websearch.SearchResponse{Results: providerResults}, "brave", nil
	}}
	recorder := runStandaloneSearchHandler(t, h, `{
		"id":"id","model":"model","commands":{"search_query":[{"q":"query"}]}
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response standaloneSearchResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Results, 5)
	require.NotContains(t, response.Output, "Result 5")
}
