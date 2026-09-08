package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// OllamaCloudMaxTokensCapExtraKey 是账号 extra 中的可选配置键，表示该 Ollama Cloud
// 账号输出 token 的 provider 级硬上限。用户可通过 admin 账号更新 API 的 extra 字段
// 设置，覆盖默认值 ollamaCloudDefaultMaxTokensCap；0 或负数表示显式禁用 clamp。
const OllamaCloudMaxTokensCapExtraKey = "ollama_max_tokens_cap"

// ollamaCloudDefaultMaxTokensCap 是 Ollama Cloud 对输出 token 的 provider 级硬上限；
// 超过会被上游 400。调用方按 DeepSeek（或 openai 平台 force_chat_completions 遗留账号）过滤。
const ollamaCloudDefaultMaxTokensCap = 65535

// clampOllamaCloudUpstreamMaxTokens 在 raw CC 出站压输出上限。判定用本次 CC 上游
// base（GetOpenAIBaseURL，含尾斜杠归一化）。DeepSeek 模型任意平台均 clamp；非
// DeepSeek 仅保留 openai 平台 force_chat_completions 账号的既有行为。
func clampOllamaCloudUpstreamMaxTokens(account *Account, body []byte) []byte {
	if account == nil || len(body) == 0 {
		return body
	}
	if !isOllamaCloudOutboundBaseURL(account.GetOpenAIBaseURL()) {
		return body
	}
	if !isDeepSeekModel(gjson.GetBytes(body, "model").String()) && !isOllamaCloudRawChatCompletionsAccount(account) {
		return body
	}
	return clampOllamaCloudMaxTokens(account, body)
}

// ollamaCloudResponsesUpstreamBaseURL 返回原生 /v1/responses 本次实际选用的上游
// base_url，取值与 buildUpstreamRequest 一致：adaptive 原生 CN 账号用 api_base_urls
// 的 responses 地址，其余用 GetOpenAIBaseURL。
func ollamaCloudResponsesUpstreamBaseURL(account *Account) string {
	if account.UsesNativeCNResponses() && account.IsAdaptiveAPIProtocol() {
		return account.GetCNProtocolBaseURL(APIProtocolResponses)
	}
	return account.GetOpenAIBaseURL()
}

// ollamaCloudResponsesMaxOutputTokensClamp 在原生 /v1/responses 路径压输出上限。
// 实际 Responses 上游为 Ollama Cloud 且出站模型为 DeepSeek 时返回 cap。
func ollamaCloudResponsesMaxOutputTokensClamp(account *Account, upstreamModel string, body []byte) (int64, bool) {
	if account == nil || account.Type != AccountTypeAPIKey || !isDeepSeekModel(upstreamModel) {
		return 0, false
	}
	if !isOllamaCloudOutboundBaseURL(ollamaCloudResponsesUpstreamBaseURL(account)) {
		return 0, false
	}
	value := gjson.GetBytes(body, "max_output_tokens")
	if !value.Exists() && account.Platform == PlatformOpenAI {
		value = gjson.GetBytes(body, "max_tokens")
	}
	cap := ollamaCloudMaxTokensCap(account)
	if cap <= 0 || !value.Exists() || value.Type != gjson.Number || value.Int() <= cap {
		return 0, false
	}
	return cap, true
}

// ollamaCloudMaxTokensCap 返回账号配置的 max_tokens 上限。账号为 nil 或 extra 中
// 无该键时返回默认值；键值为数值类型（float64/int64/int/json.Number）时返回其整数
// 值（0 或负数表示显式禁用 clamp）；其它类型回退默认值。
func ollamaCloudMaxTokensCap(account *Account) int64 {
	if account == nil || account.Extra == nil {
		return ollamaCloudDefaultMaxTokensCap
	}
	value, ok := account.Extra[OllamaCloudMaxTokensCapExtraKey]
	if !ok {
		return ollamaCloudDefaultMaxTokensCap
	}
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int64:
		return number
	case int:
		return int64(number)
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return ollamaCloudDefaultMaxTokensCap
		}
		return parsed
	default:
		return ollamaCloudDefaultMaxTokensCap
	}
}

// clampOllamaCloudMaxTokens 把 body 中超过 cap 的 max_tokens / max_completion_tokens
// 单向压到 cap。cap <= 0 或 body 不是合法 JSON 时原样返回；sjson 出错时返回原始 body。
// 有任一字段被 clamp 时记录一条 Debug 日志。
func clampOllamaCloudMaxTokens(account *Account, body []byte) []byte {
	cap := ollamaCloudMaxTokensCap(account)
	if cap <= 0 || !gjson.ValidBytes(body) {
		return body
	}
	clamped := false
	out := body
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		result := gjson.GetBytes(out, key)
		if !result.Exists() || result.Type != gjson.Number || result.Int() <= cap {
			continue
		}
		updated, err := sjson.SetBytes(out, key, cap)
		if err != nil {
			return body
		}
		out = updated
		clamped = true
	}
	if clamped && account != nil {
		logger.L().Debug("openai chat_completions raw: clamped max_tokens for ollama cloud account",
			zap.Int64("account_id", account.ID),
			zap.Int64("cap", cap),
		)
	}
	return out
}
