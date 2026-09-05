package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// newAliyunCaptchaTestTarget 起一个假的阿里云端点，让真实 SDK 走完整的签名/序列化链路。
func newAliyunCaptchaTestTarget(t *testing.T, handler http.HandlerFunc) (*aliyunCaptchaVerifier, service.AliyunCaptchaCredentials) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	verifier := &aliyunCaptchaVerifier{protocol: "HTTP", timeoutMillis: 2_000}
	cred := service.AliyunCaptchaCredentials{
		AccessKeyID:     "test-ak-id",
		AccessKeySecret: "test-ak-secret",
		SceneID:         "scene-1",
		Endpoint:        strings.TrimPrefix(server.URL, "http://"),
	}
	return verifier, cred
}

func TestAliyunCaptchaVerifier_VerifySuccess(t *testing.T) {
	var capturedParam, capturedSceneID string
	verifier, cred := newAliyunCaptchaTestTarget(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		capturedParam = r.Form.Get("CaptchaVerifyParam")
		capturedSceneID = r.Form.Get("SceneId")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"Success","Message":"success","RequestId":"req-1","Success":true,"Result":{"VerifyResult":true,"VerifyCode":"T001"}}`))
	})

	result, err := verifier.VerifyCaptcha(context.Background(), cred, "the-verify-param")
	require.NoError(t, err)
	require.True(t, result.VerifyResult)
	require.Equal(t, "T001", result.VerifyCode)
	require.Equal(t, "the-verify-param", capturedParam)
	require.Equal(t, "scene-1", capturedSceneID)
}

func TestAliyunCaptchaVerifier_VerifyResultFalse(t *testing.T) {
	verifier, cred := newAliyunCaptchaTestTarget(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"Success","RequestId":"req-2","Success":true,"Result":{"VerifyResult":false,"VerifyCode":"F002"}}`))
	})

	result, err := verifier.VerifyCaptcha(context.Background(), cred, "bad-param")
	require.NoError(t, err)
	require.False(t, result.VerifyResult)
	require.Equal(t, "F002", result.VerifyCode)
}

func TestAliyunCaptchaVerifier_APIErrorNormalized(t *testing.T) {
	verifier, cred := newAliyunCaptchaTestTarget(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"Code":"SignatureDoesNotMatch","Message":"Specified signature is not matched with our calculation.","RequestId":"req-3"}`))
	})

	_, err := verifier.VerifyCaptcha(context.Background(), cred, "param")
	require.Error(t, err)
	var apiErr *service.AliyunCaptchaAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "SignatureDoesNotMatch", apiErr.Code)
}

func TestAliyunCaptchaVerifier_TransportError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := strings.TrimPrefix(server.URL, "http://")
	server.Close() // 立即关闭，制造连接失败

	verifier := &aliyunCaptchaVerifier{protocol: "HTTP", timeoutMillis: 2_000}
	cred := service.AliyunCaptchaCredentials{
		AccessKeyID:     "test-ak-id",
		AccessKeySecret: "test-ak-secret",
		SceneID:         "scene-1",
		Endpoint:        endpoint,
	}

	_, err := verifier.VerifyCaptcha(context.Background(), cred, "param")
	require.Error(t, err)
	var apiErr *service.AliyunCaptchaAPIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport errors must not be normalized to API errors: code=%q message=%q err=%v", apiErr.Code, apiErr.Message, err)
	}
}

func TestAliyunCaptchaVerifier_HTTP503WithoutOpenAPICode(t *testing.T) {
	verifier, cred := newAliyunCaptchaTestTarget(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{}`))
	})

	_, err := verifier.VerifyCaptcha(context.Background(), cred, "param")
	require.Error(t, err)
	var apiErr *service.AliyunCaptchaAPIError
	require.False(t, errors.As(err, &apiErr), "HTTP 503 without an OpenAPI code must not become AliyunCaptchaAPIError: %v", err)
}

func TestNormalizeAliyunCaptchaError(t *testing.T) {
	t.Parallel()

	signatureMismatch := tea.NewSDKError(map[string]any{
		"code":       "SignatureDoesNotMatch",
		"message":    "Specified signature is not matched with our calculation.",
		"statusCode": 403,
	})
	unreachable := tea.NewSDKError(map[string]any{
		"code":       "<nil>",
		"message":    "code: 503, <nil> request id: <nil>",
		"statusCode": 503,
	})
	numericTransport := tea.NewSDKError(map[string]any{
		"code":    503,
		"message": "connection refused",
	})
	emptyCode := tea.NewSDKError(map[string]any{
		"message": "i/o timeout",
	})
	daraUnreachable := dara.NewSDKError(map[string]any{
		"code":       "<nil>",
		"message":    "code: 503, connection refused request id: <nil>",
		"statusCode": 503,
	})
	daraAPI := dara.NewSDKError(map[string]any{
		"code":       "InvalidAccessKeyId.NotFound",
		"message":    "specified access key is not found",
		"statusCode": 404,
	})
	plain := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")

	tests := []struct {
		name    string
		err     error
		wantAPI bool
		code    string
	}{
		{name: "tea openapi business code", err: signatureMismatch, wantAPI: true, code: "SignatureDoesNotMatch"},
		{name: "dara openapi business code", err: daraAPI, wantAPI: true, code: "InvalidAccessKeyId.NotFound"},
		{name: "tea placeholder nil code is transport", err: unreachable, wantAPI: false},
		{name: "tea numeric http status is transport", err: numericTransport, wantAPI: false},
		{name: "tea empty code is transport", err: emptyCode, wantAPI: false},
		{name: "dara placeholder nil code is transport", err: daraUnreachable, wantAPI: false},
		{name: "plain network error", err: plain, wantAPI: false},
		{name: "wrapped tea placeholder code is transport", err: fmt.Errorf("captcha: %w", unreachable), wantAPI: false},
		{name: "dara numeric http status is transport", err: dara.NewSDKError(map[string]any{
			"code":    503,
			"message": "connection refused",
		}), wantAPI: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeAliyunCaptchaError(tt.err)
			require.Error(t, got)
			var apiErr *service.AliyunCaptchaAPIError
			isAPI := errors.As(got, &apiErr)
			require.Equal(t, tt.wantAPI, isAPI, "err=%v", got)
			if tt.wantAPI {
				require.Equal(t, tt.code, apiErr.Code)
			} else {
				require.Equal(t, tt.err, got)
			}
		})
	}
}
