package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newZhipuCodingProbeAccount(baseURL, apiKey string) *Account {
	creds := map[string]any{
		"account_mode": AccountModeCoding,
		"api_key":      apiKey,
	}
	if baseURL != "" {
		creds["base_url"] = baseURL
	}
	return &Account{
		ID:          11,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Credentials: creds,
	}
}

func TestCNProviderQuotaService_AuthFailureMarksCredentialInvalid(t *testing.T) {
	repo := &fakeCNProbeAccountRepo{account: newZhipuCodingProbeAccount("https://open.bigmodel.cn/api/coding/paas/v4", "sk-official")}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(`{"success":false,"msg":"invalid api key"}`)),
	}}
	svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

	result, err := svc.QueryUsage(context.Background(), 11)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, result.CredentialValid)
	require.Equal(t, http.StatusUnauthorized, result.StatusCode)
	require.Contains(t, result.Error, "401")
}

func TestCNProviderQuotaService_TransientHTTPErrorIsNotAuthFailure(t *testing.T) {
	repo := &fakeCNProbeAccountRepo{account: newZhipuCodingProbeAccount("https://open.bigmodel.cn/api/coding/paas/v4", "sk-official")}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("bad gateway")),
	}}
	svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

	result, err := svc.QueryUsage(context.Background(), 11)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.True(t, result.CredentialValid, "5xx must not be treated as a confirmed credential failure")
	require.Equal(t, http.StatusBadGateway, result.StatusCode)
}

func TestCNProviderQuotaService_ZhipuBusinessErrorIsNotAuthFailure(t *testing.T) {
	repo := &fakeCNProbeAccountRepo{account: newZhipuCodingProbeAccount("https://open.bigmodel.cn/api/coding/paas/v4", "sk-official")}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":false,"msg":"quota service busy"}`)),
	}}
	svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

	result, err := svc.QueryUsage(context.Background(), 11)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.True(t, result.CredentialValid)
	require.Contains(t, result.Error, "quota service busy")
}

func TestCNProviderQuotaService_CustomZhipuBaseURLStaysOnSameHost(t *testing.T) {
	repo := &fakeCNProbeAccountRepo{account: newZhipuCodingProbeAccount("https://relay.example.com/api/paas/v4", "sk-relay")}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"level":"pro","limits":[]}}`)),
	}}
	svc := NewCNProviderQuotaService(repo, nil, upstream, nil)

	result, err := svc.QueryUsage(context.Background(), 11)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://relay.example.com/api/monitor/usage/quota/limit", upstream.lastReq.URL.String())
	require.NotContains(t, upstream.lastReq.URL.Host, "bigmodel.cn")
}

func TestCNProviderQuotaService_CustomZhipuHostRespectsAllowlist(t *testing.T) {
	repo := &fakeCNProbeAccountRepo{account: newZhipuCodingProbeAccount("https://relay.example.com/api/paas/v4", "sk-relay")}
	upstream := &recordingHTTPUpstream{}
	svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("relay.example.com"))

	_, err := svc.QueryUsage(context.Background(), 11)
	require.Error(t, err)
	// 请求到达 recording stub 说明没有被改写成未放行的官网主机。
	require.Equal(t, 1, upstream.calls)
}
