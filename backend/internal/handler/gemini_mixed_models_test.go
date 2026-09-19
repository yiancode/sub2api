package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// geminiAntigravityListErrorRepo fails only the Antigravity-only listing used by
// AntigravityGeminiModelIDs, so native Gemini selection can still proceed.
type geminiAntigravityListErrorRepo struct {
	geminiAllowlistAccountRepoStub
	err error
}

func (s *geminiAntigravityListErrorRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	if s.err != nil && len(platforms) == 1 && platforms[0] == service.PlatformAntigravity {
		return nil, s.err
	}
	return s.geminiAllowlistAccountRepoStub.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, platforms)
}

func TestGeminiNativeModelsUsesAccountMappings(t *testing.T) {
	for _, tt := range []struct {
		name                     string
		forced, mixed, allowlist bool
		status                   int
	}{
		{"mixed", false, true, false, 200},
		{"mixed allowlist", false, true, true, 200},
		{"disabled mixed", false, false, false, 503},
		{"forced without mixed opt-in", true, false, false, 200},
		{"forced allowlist", true, false, true, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(45)
			repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				groupID: {{ID: 1, Platform: service.PlatformAntigravity,
					Extra:       map[string]any{"mixed_scheduling": tt.mixed},
					Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high", "claude-custom": "claude-sonnet-4-6"}}}},
			}}}
			h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, nil)}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformGemini,
				ModelAllowlist: service.GroupModelAllowlist{Enabled: tt.allowlist, Models: []string{"gemini-synced-custom"}},
			}})
			if tt.forced {
				c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)
			}
			h.GeminiV1BetaListModels(c)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.status != 200 {
				return
			}
			var got gemini.ModelsListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			names := []string{}
			for _, model := range got.Models {
				names = append(names, model.Name)
				require.Contains(t, model.SupportedGenerationMethods, "generateContent")
			}
			require.Contains(t, names, "models/gemini-synced-custom")
			require.NotContains(t, names, "models/claude-custom")
			require.NotContains(t, names, "models/gemini-2.0-flash")
			if tt.allowlist {
				require.Equal(t, []string{"models/gemini-synced-custom"}, names)
			}
		})
	}
}

func TestAppendUpstreamGeminiModelsPreservesMetadata(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}}],"nextPageToken":"next","other":42}`)
	extra := []gemini.Model{gemini.FallbackModel("gemini-native"), gemini.FallbackModel("gemini-synced-custom"), gemini.FallbackModel("gemini-synced-custom")}
	merged, ok := appendUpstreamGeminiModels(body, extra)
	require.True(t, ok)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}},{"name":"models/gemini-synced-custom","supportedGenerationMethods":["generateContent","streamGenerateContent"]}],"nextPageToken":"next","other":42}`, string(merged))
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(merged, service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-synced-*"}})
	require.True(t, ok)
	require.True(t, dropped)
	require.NotContains(t, string(filtered), "gemini-native")
	require.Contains(t, string(filtered), "gemini-synced-custom")
	for _, invalid := range []string{`null`, `not-json`, `{"error":"bad"}`, `{"models":{}}`} {
		got, ok := appendUpstreamGeminiModels([]byte(invalid), extra)
		require.False(t, ok)
		require.Equal(t, invalid, string(got))
	}
}

// Exercise the real handler's native-upstream branch, including scope fallback.
type geminiMixedModelsUpstream struct {
	service.HTTPUpstream
	status int
	body   string
}

func (u *geminiMixedModelsUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"native-id"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func TestGeminiNativeModelsMergesNativeUpstream(t *testing.T) {
	for _, tt := range []struct {
		name           string
		status         int
		body           string
		expectedNative string
	}{
		{"native", 200, `{"models":[{"name":"models/gemini-native","inputTokenLimit":123}],"nextPageToken":"next"}`, "models/gemini-native"},
		{"scope fallback", 403, `{"error":"insufficient authentication scopes"}`, "models/gemini-2.5-pro"},
		{"upstream error", 429, `{"error":"rate limited"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(46)
			repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
				{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}},
				{ID: 2, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high"}}},
			}}}}
			upstream := &geminiMixedModelsUpstream{status: tt.status, body: tt.body}
			h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGemini}})
			h.GeminiV1BetaListModels(c)
			if tt.status == 429 {
				require.Equal(t, 429, rec.Code)
				require.JSONEq(t, tt.body, rec.Body.String())
				return
			}
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.expectedNative)
			require.Contains(t, rec.Body.String(), "models/gemini-synced-custom")
			if tt.status == 200 {
				require.Contains(t, rec.Body.String(), `"inputTokenLimit":123`)
				require.Contains(t, rec.Body.String(), `"nextPageToken":"next"`)
				require.Equal(t, "native-id", rec.Header().Get("X-Request-Id"))
			}
		})
	}
}

func TestGeminiV1BetaListModels_AntigravityListingFailureDoesNotBlockNative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := int64(47)
	repo := &geminiAntigravityListErrorRepo{
		geminiAllowlistAccountRepoStub: geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
			{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}},
			{ID: 2, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high"}}},
		}}}},
		err: errors.New("antigravity account store unavailable"),
	}
	upstream := &geminiMixedModelsUpstream{status: 200, body: `{"models":[{"name":"models/gemini-native","inputTokenLimit":123}]}`}
	h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGemini}})
	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEqual(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), "Unable to list Antigravity models")
	require.NotContains(t, rec.Body.String(), "antigravity account store unavailable")
	require.Contains(t, rec.Body.String(), "models/gemini-native")
}

func TestGeminiV1BetaListModels_AntigravityListingFailureDoesNotExpose503WhenNativeAlsoMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := int64(48)
	repo := &geminiAntigravityListErrorRepo{
		geminiAllowlistAccountRepoStub: geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
			{ID: 2, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high"}}},
		}}}},
		err: errors.New("antigravity account store unavailable"),
	}
	h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, &config.Config{})}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGemini}})
	h.GeminiV1BetaListModels(c)

	require.NotEqual(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.NotEqual(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "Unable to list Antigravity models")
	require.NotContains(t, rec.Body.String(), "antigravity account store unavailable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"models":[]`)
	var got gemini.ModelsListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.Models)
}

func TestGeminiV1BetaListModels_AntigravityListingFailureDoesNotExposeNativeGETFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := int64(49)
	repo := &geminiAntigravityListErrorRepo{
		geminiAllowlistAccountRepoStub: geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
			{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}},
		}}}},
		err: errors.New("antigravity account store unavailable"),
	}
	cases := []struct {
		name     string
		status   int
		body     string
		upstream service.HTTPUpstream
	}{
		{name: "transport", upstream: geminiFailingModelsUpstream{err: errors.New("dial tcp: i/o timeout")}},
		{name: "upstream 500", status: 500, body: `{"error":{"message":"gemini internal"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := tc.upstream
			if upstream == nil {
				upstream = &geminiMixedModelsUpstream{status: tc.status, body: tc.body}
			}
			h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGemini}})
			h.GeminiV1BetaListModels(c)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.NotEqual(t, http.StatusBadGateway, rec.Code)
			require.NotEqual(t, http.StatusServiceUnavailable, rec.Code)
			require.NotContains(t, rec.Body.String(), "i/o timeout")
			require.NotContains(t, rec.Body.String(), "gemini internal")
			require.Contains(t, rec.Body.String(), `"models":[]`)
		})
	}
}

type geminiFailingModelsUpstream struct {
	service.HTTPUpstream
	err error
}

func (u geminiFailingModelsUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, u.err
}
