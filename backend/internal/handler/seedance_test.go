//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSeedanceHandlerLifecycleAndOwnership(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	var owner int64
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		body := `{"id":"task-ark","status":"queued"}`
		if req.Method == http.MethodPost {
			owner = id
		} else {
			require.Equal(t, owner, id)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	newContext := func(method string) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformOpenAI
		body := ""
		if method == http.MethodPost {
			body = `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`
		}
		c.Request = httptest.NewRequest(method, "/api/v3/contents/generations/tasks", strings.NewReader(body))
		c.Params = gin.Params{{Key: "task_id", Value: "task-ark"}}
		return c, w
	}
	c, w := newContext(http.MethodPost)
	h.SeedanceTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Positive(t, owner)
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		c, w = newContext(method)
		h.SeedanceTasks(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		slots.assertReleased(t)
	}
	for _, other := range []string{"user", "key", "group", "task", "provider"} {
		c, w = newContext(http.MethodGet)
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11, Concurrency: 5})
		case "key":
			key.ID = 21
		case "group":
			group := int64(25)
			key.GroupID = &group
		case "task":
			c.Params = gin.Params{{Key: "task_id", Value: "other"}}
		case "provider":
			c.Params = gin.Params{{Key: "request_id", Value: "task-ark"}}
		}
		before := upstream.calls
		if other == "provider" {
			h.GrokVideoStatus(c)
		} else {
			h.SeedanceTasks(c)
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, before, upstream.calls)
		slots.assertReleased(t)
	}
	c, _ = newContext(http.MethodGet)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 12345}, ResponseID: "seedance:task-ark"}
	for i := range 20 {
		billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, result.ResponseID, result)
		if i == 0 {
			require.NotNil(t, billed)
			require.Equal(t, "doubao-seedance", billed.BillingModel)
			require.Equal(t, 12345, billed.Usage.OutputTokens)
			require.Zero(t, billed.VideoCount)
		} else {
			require.Nil(t, billed)
		}
	}
	require.Len(t, bindings.billed, 1)
}

func newSeedanceSlotHandler(t *testing.T, capable bool) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *grokMediaSlotUpstream) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 3)
	for i := range accounts {
		creds := map[string]any{"api_key": "test-key", "base_url": "https://ark.cn-beijing.volces.com/api/v3"}
		if capable {
			creds["openai_capabilities"] = []string{"seedance"}
		}
		accounts[i] = service.Account{
			ID: int64(i + 1), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i,
			GroupIDs: []int64{24}, Credentials: creds,
		}
	}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	bindings := &grokMediaSlotBindings{owner: 1}
	upstream := &grokMediaSlotUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"task-ark","status":"queued"}`))}, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	repo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: accounts}}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, nil, nil, nil, upstream, nil, nil, service.NewGrokTokenProvider(repo, nil), nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	return handler, slots, upstream
}

func seedanceCreateContext() (*gin.Context, *httptest.ResponseRecorder) {
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = service.PlatformOpenAI
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestSeedanceCreateWithoutEligibleAccountDoesNotExpose503(t *testing.T) {
	h, slots, upstream := newSeedanceSlotHandler(t, false)
	c, w := seedanceCreateContext()
	h.SeedanceTasks(c)
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotEqual(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "seedance_no_eligible_account")
	require.NotContains(t, w.Body.String(), "No eligible Seedance accounts")
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "model_not_found")
	require.Zero(t, upstream.calls)
	slots.assertReleased(t)
}

func TestSeedanceCreateDoesNotExposeUpstreamPlatformFailures(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		err      error
		leak     string
		wantCode int
	}{
		{name: "upstream 500", status: 500, body: `{"error":{"message":"ark internal"}}`, leak: "ark internal", wantCode: http.StatusBadRequest},
		{name: "upstream 429", status: 429, body: `{"error":{"code":"QuotaExceeded","message":"quota exhausted"}}`, leak: "QuotaExceeded", wantCode: http.StatusBadRequest},
		{name: "transport", err: errors.New("connection reset"), leak: "connection reset", wantCode: http.StatusBadRequest},
		{name: "missing task id", status: 200, body: `{"status":"queued"}`, wantCode: http.StatusBadRequest},
		{name: "user 400", status: 400, body: `{"error":{"code":"InvalidParameter","message":"content is required"}}`, wantCode: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, slots, upstream := newSeedanceSlotHandler(t, true)
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}
			c, w := seedanceCreateContext()
			h.SeedanceTasks(c)
			require.NotEqual(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			require.NotEqual(t, http.StatusBadGateway, w.Code, w.Body.String())
			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			if tc.name == "user 400" {
				require.Contains(t, w.Body.String(), "InvalidParameter")
			} else if tc.leak != "" {
				require.NotContains(t, w.Body.String(), tc.leak)
			}
			slots.assertReleased(t)
		})
	}
}
