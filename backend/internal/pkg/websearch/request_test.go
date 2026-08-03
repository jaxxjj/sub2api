package websearch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateSearchRequest(t *testing.T) {
	require.NoError(t, ValidateSearchRequest(SearchRequest{
		Query: "query", MaxResults: 10, Domains: []string{"openai.com"}, RecencyDays: 30,
	}))

	for _, req := range []SearchRequest{
		{Query: ""},
		{Query: "query", MaxResults: 21},
		{Query: "query", Domains: []string{"https://openai.com"}},
		{Query: "query", Domains: []string{"openai.com/path"}},
		{Query: "query", RecencyDays: -1},
		{Query: "query", RecencyDays: 3_651},
	} {
		require.Error(t, ValidateSearchRequest(req), "request=%+v", req)
	}
}

func TestBraveFreshness(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "pd", braveFreshness(1, now))
	require.Equal(t, "pw", braveFreshness(7, now))
	require.Equal(t, "2026-07-04to2026-08-03", braveFreshness(30, now))
}
