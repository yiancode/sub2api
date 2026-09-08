package service

import (
	"github.com/tidwall/gjson"
)

// clampOllamaCloudAnthropicMessagesMaxTokens 在 Anthropic Messages 出站压输出上限。
// baseURL 必须是本次 Anthropic 上游（GetBaseURL / GetAnthropicProtocolBaseURL），
// 不能用 adaptive 账号的 CC/Responses 地址。
func clampOllamaCloudAnthropicMessagesMaxTokens(account *Account, baseURL string, body []byte) []byte {
	if account == nil || len(body) == 0 {
		return body
	}
	if !isOllamaCloudOutboundBaseURL(baseURL) {
		return body
	}
	if !isDeepSeekModel(gjson.GetBytes(body, "model").String()) {
		return body
	}
	return clampOllamaCloudMaxTokens(account, body)
}
