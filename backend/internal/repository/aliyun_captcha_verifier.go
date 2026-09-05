package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	captcha "github.com/alibabacloud-go/captcha-20230305/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const aliyunCaptchaTimeoutMillis = 10_000

type aliyunCaptchaVerifier struct {
	protocol      string // "HTTPS"；测试注入 "HTTP" 指向 httptest.Server
	timeoutMillis int
}

func NewAliyunCaptchaVerifier() service.AliyunCaptchaVerifier {
	return &aliyunCaptchaVerifier{
		protocol:      "HTTPS",
		timeoutMillis: aliyunCaptchaTimeoutMillis,
	}
}

// VerifyCaptcha 调用阿里云验证码 2.0 VerifyIntelligentCaptcha。
// AK/SK 是可热更的后台设置，每次调用按当前凭证新建 client。
func (v *aliyunCaptchaVerifier) VerifyCaptcha(ctx context.Context, cred service.AliyunCaptchaCredentials, captchaVerifyParam string) (*service.AliyunCaptchaVerifyResult, error) {
	client, err := captcha.NewClient(&openapiutil.Config{
		AccessKeyId:     dara.String(cred.AccessKeyID),
		AccessKeySecret: dara.String(cred.AccessKeySecret),
		Endpoint:        dara.String(cred.Endpoint),
		Protocol:        dara.String(v.protocol),
		ConnectTimeout:  dara.Int(v.timeoutMillis),
		ReadTimeout:     dara.Int(v.timeoutMillis),
	})
	if err != nil {
		return nil, fmt.Errorf("create aliyun captcha client: %w", err)
	}

	request := &captcha.VerifyIntelligentCaptchaRequest{
		CaptchaVerifyParam: dara.String(captchaVerifyParam),
		SceneId:            dara.String(cred.SceneID),
	}

	response, err := client.VerifyIntelligentCaptchaWithContext(ctx, request, &dara.RuntimeOptions{})
	if err != nil {
		return nil, normalizeAliyunCaptchaError(err)
	}

	result := &service.AliyunCaptchaVerifyResult{}
	if body := response.Body; body != nil && body.Result != nil {
		result.VerifyResult = dara.BoolValue(body.Result.VerifyResult)
		result.VerifyCode = dara.StringValue(body.Result.VerifyCode)
	}
	return result, nil
}

// normalizeAliyunCaptchaError 把带 OpenAPI 业务码的 SDK 错误归一化为
// service.AliyunCaptchaAPIError。tea/dara 也会把连不上、超时等包装成 SDKError
// （code 为 "<nil>"、空，或纯数字 HTTP 状态如 503），那些必须原样返回，
// 不能变成 AliyunCaptchaAPIError，以免后续 errors.As 把它当成 OpenAPI 业务错误。
func normalizeAliyunCaptchaError(err error) error {
	code, message, ok := aliyunCaptchaOpenAPIError(err)
	if !ok {
		return err
	}
	return &service.AliyunCaptchaAPIError{
		Code:    code,
		Message: message,
	}
}

func aliyunCaptchaOpenAPIError(err error) (code, message string, ok bool) {
	var teaErr *tea.SDKError
	if errors.As(err, &teaErr) {
		code = tea.StringValue(teaErr.Code)
		message = tea.StringValue(teaErr.Message)
	}
	var daraErr *dara.SDKError
	if errors.As(err, &daraErr) {
		if c := dara.StringValue(daraErr.Code); c != "" {
			code = c
		}
		if m := dara.StringValue(daraErr.Message); m != "" {
			message = m
		}
	}
	if !isAliyunCaptchaOpenAPIErrorCode(code) {
		return "", "", false
	}
	if message == "" {
		message = err.Error()
	}
	return code, message, true
}

func isAliyunCaptchaOpenAPIErrorCode(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" || code == "<nil>" || strings.EqualFold(code, "nil") {
		return false
	}
	if _, convErr := strconv.Atoi(code); convErr == nil {
		return false
	}
	return true
}
