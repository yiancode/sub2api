package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractUpstreamErrorMessage_GrokFlatEnvelope(t *testing.T) {
	t.Parallel()

	got := extractUpstreamErrorMessage([]byte(
		`{"code":"invalid-argument","error":"This model's maximum prompt length is 500000 but the request contains 500323 tokens."}`,
	))
	require.Equal(t, "This model's maximum prompt length is 500000 but the request contains 500323 tokens.", got)

	got = extractUpstreamErrorMessage([]byte(
		`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 100 tokens > 50 maximum"}}`,
	))
	require.Equal(t, "prompt is too long: 100 tokens > 50 maximum", got)

	got = extractUpstreamErrorMessage([]byte(`{"error":{"type":"invalid_request_error"}}`))
	require.Empty(t, got)
}
