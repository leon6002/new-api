package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"one-api/common"
	"one-api/constant"
	"one-api/dto"
	"one-api/model"
	relaycommon "one-api/relay/common"
	relayconstant "one-api/relay/constant"
	"one-api/service"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getAndValidateTextRequest 从 gin 上下文中获取并验证文本请求
// c: gin 的上下文对象，用于获取 HTTP 请求信息和返回响应。
// relayInfo: 包含中继模式信息的 RelayInfo 结构体。
// 返回值: 验证通过后的 GeneralOpenAIRequest 结构体指针和可能出现的错误。
func getAndValidateTextRequest(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	textRequest := &dto.GeneralOpenAIRequest{}
	// 从 HTTP 请求体中反序列化 JSON 数据到 textRequest
	err := common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}

	// 根据中继模式和请求情况，设置默认的 Model 参数值
	if relayInfo.RelayMode == relayconstant.RelayModeModerations && textRequest.Model == "" {
		textRequest.Model = "text-moderation-latest"
	}
	if relayInfo.RelayMode == relayconstant.RelayModeEmbeddings && textRequest.Model == "" {
		textRequest.Model = c.Param("model")
	}

	// 验证 MaxTokens 参数的合法性
	if textRequest.MaxTokens < 0 || textRequest.MaxTokens > math.MaxInt32/2 {
		return nil, errors.New("max_tokens is invalid")
	}
	// 验证 Model 参数是否为空
	if textRequest.Model == "" {
		return nil, errors.New("model is required")
	}

	// 根据不同的中继模式，验证请求中必需的字段
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeCompletions:
		if textRequest.Prompt == "" {
			return nil, errors.New("field prompt is required")
		}
	case relayconstant.RelayModeChatCompletions:
		if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
			return nil, errors.New("field messages is required")
		}
	case relayconstant.RelayModeEmbeddings:
		// 此模式下可能不需要额外的验证
	case relayconstant.RelayModeModerations:
		if textRequest.Input == "" {
			return nil, errors.New("field input is required")
		}
	case relayconstant.RelayModeEdits:
		if textRequest.Instruction == "" {
			return nil, errors.New("field instruction is required")
		}
	}

	// 设置是否启用流式响应
	relayInfo.IsStream = textRequest.Stream
	return textRequest, nil
}

// TextHelper 处理文本请求的助手函数
//
// 参数:
// c *gin.Context: Gin框架的上下文对象，用于处理HTTP请求和响应
//
// 返回值:
// *dto.OpenAIErrorWithStatusCode: 包装了OpenAI错误信息和HTTP状态码的结构体指针，用于向客户端返回错误信息
func TextHelper(c *gin.Context) *dto.OpenAIErrorWithStatusCode {

	relayInfo := relaycommon.GenRelayInfo(c) // 生成中继信息

	// 获取并验证文本请求
	textRequest, err := getAndValidateTextRequest(c, relayInfo)
	if err != nil {
		common.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return service.OpenAIErrorWrapper(err, "invalid_text_request", http.StatusBadRequest)
	}

	// 映射模型名称
	modelMapping := c.GetString("model_mapping")
	isModelMapped := false
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
		}
		if modelMap[textRequest.Model] != "" {
			textRequest.Model = modelMap[textRequest.Model]
			// 设置上游模型名称
			isModelMapped = true
		}
	}
	relayInfo.UpstreamModelName = textRequest.Model
	modelPrice := common.GetModelPrice(textRequest.Model, false)
	groupRatio := common.GetGroupRatio(relayInfo.Group)

	var preConsumedQuota int
	var ratio float64
	var modelRatio float64

	// 获取prompt令牌并进行敏感词检查
	promptTokens, err, sensitiveTrigger := getPromptTokens(textRequest, relayInfo)

	// 计算prompt令牌错误
	if err != nil {
		if sensitiveTrigger {
			return service.OpenAIErrorWrapper(err, "sensitive_words_detected", http.StatusBadRequest)
		}
		return service.OpenAIErrorWrapper(err, "count_token_messages_failed", http.StatusInternalServerError)
	}

	// 处理模型价格未知的情况，计算预消耗的配额
	if modelPrice == -1 {
		preConsumedTokens := common.PreConsumedQuota
		if textRequest.MaxTokens != 0 {
			preConsumedTokens = promptTokens + int(textRequest.MaxTokens)
		}
		modelRatio = common.GetModelRatio(textRequest.Model)
		ratio = modelRatio * groupRatio
		preConsumedQuota = int(float64(preConsumedTokens) * ratio)
	} else {
		preConsumedQuota = int(modelPrice * common.QuotaPerUnit * groupRatio)
	}

	// 预消耗配额
	preConsumedQuota, userQuota, openaiErr := preConsumeQuota(c, preConsumedQuota, relayInfo)
	if openaiErr != nil {
		return openaiErr
	}

	// 获取适配器并初始化
	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return service.OpenAIErrorWrapper(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(relayInfo, *textRequest)

	var requestBody io.Reader
	// 根据API类型准备请求体
	if relayInfo.ApiType == relayconstant.APITypeOpenAI {
		if isModelMapped {
			jsonStr, err := json.Marshal(textRequest)
			if err != nil {
				return service.OpenAIErrorWrapper(err, "marshal_text_request_failed", http.StatusInternalServerError)
			}
			requestBody = bytes.NewBuffer(jsonStr)
		} else {
			requestBody = c.Request.Body
		}
	} else {
		convertedRequest, err := adaptor.ConvertRequest(c, relayInfo.RelayMode, textRequest)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "convert_request_failed", http.StatusInternalServerError)
		}
		jsonData, err := json.Marshal(convertedRequest)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "json_marshal_failed", http.StatusInternalServerError)
		}
		requestBody = bytes.NewBuffer(jsonData)
	}

	// 执行HTTP请求
	resp, err := adaptor.DoRequest(c, relayInfo, requestBody)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	relayInfo.IsStream = relayInfo.IsStream || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")

	// 处理非200响应
	if resp.StatusCode != http.StatusOK {
		returnPreConsumedQuota(c, relayInfo.TokenId, userQuota, preConsumedQuota)
		return service.RelayErrorHandler(resp)
	}

	// 处理响应体
	usage, openaiErr, sensitiveResp := adaptor.DoResponse(c, resp, relayInfo)
	if openaiErr != nil {
		if sensitiveResp == nil { // 没有敏感词检查结果
			returnPreConsumedQuota(c, relayInfo.TokenId, userQuota, preConsumedQuota)
			return openaiErr
		} else {
			// 有敏感词检查结果，消耗配额
			postConsumeQuota(c, relayInfo, *textRequest, usage, ratio, preConsumedQuota, userQuota, modelRatio, groupRatio, modelPrice, sensitiveResp)
			if constant.StopOnSensitiveEnabled { // 是否直接返回错误
				return openaiErr
			}
			return nil
		}
	}
	// 消耗配额
	postConsumeQuota(c, relayInfo, *textRequest, usage, ratio, preConsumedQuota, userQuota, modelRatio, groupRatio, modelPrice, nil)
	return nil
}

// getPromptTokens 根据不同的 relay 模式计算 prompt 中的 token 数量，并检查是否触发敏感内容。
//
// 参数:
// - textRequest: 包含消息、prompt 或输入内容以及模型信息的请求DTO。
// - info: 包含 relay 模式和其他上下文信息的 Relay 信息。
//
// 返回值:
// - int: 计算得到的 prompt 中的 token 数量。
// - error: 在处理过程中遇到的任何错误。
// - bool: 指示是否触发了敏感内容的标志。
func getPromptTokens(textRequest *dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) (int, error, bool) {
	var promptTokens int
	var err error
	var sensitiveTrigger bool
	// 检查是否需要对 prompt 进行敏感内容检查
	checkSensitive := constant.ShouldCheckPromptSensitive()
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions:
		// 对聊天完成消息中的 token 进行计数
		promptTokens, err, sensitiveTrigger = service.CountTokenMessages(textRequest.Messages, textRequest.Model, checkSensitive)
	case relayconstant.RelayModeCompletions:
		// 对 completions 模式下的 prompt 进行 token 计数
		promptTokens, err, sensitiveTrigger = service.CountTokenInput(textRequest.Prompt, textRequest.Model, checkSensitive)
	case relayconstant.RelayModeModerations:
		// 对 modulations 模式下的输入进行 token 计数
		promptTokens, err, sensitiveTrigger = service.CountTokenInput(textRequest.Input, textRequest.Model, checkSensitive)
	case relayconstant.RelayModeEmbeddings:
		// 对 embeddings 模式下的输入进行 token 计数
		promptTokens, err, sensitiveTrigger = service.CountTokenInput(textRequest.Input, textRequest.Model, checkSensitive)
	default:
		// 处理未知 relay 模式的情况
		err = errors.New("unknown relay mode")
		promptTokens = 0
	}
	// 更新 relay 信息中的 prompt token 数量
	info.PromptTokens = promptTokens
	return promptTokens, err, sensitiveTrigger
}

// 预扣费并返回用户剩余配额
// preConsumeQuota 预消费配额函数
// 在执行具体操作前，检查并预先消费用户和令牌的配额。
// 参数:
// - c *gin.Context: Gin框架的上下文对象，用于获取请求信息和记录日志。
// - preConsumedQuota int: 预先消费的配额数量。
// - relayInfo *relaycommon.RelayInfo: 包含用户ID、令牌ID和是否无限令牌的信息。
// 返回值:
// - int: 实际预消费的配额数量。
// - int: 操作后用户的剩余配额。
// - *dto.OpenAIErrorWithStatusCode: 如果有错误发生，返回错误信息和HTTP状态码。
func preConsumeQuota(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) (int, int, *dto.OpenAIErrorWithStatusCode) {
	// 尝试从缓存获取用户配额
	userQuota, err := model.CacheGetUserQuota(relayInfo.UserId)
	if err != nil {
		// 获取用户配额失败，返回错误
		return 0, 0, service.OpenAIErrorWrapper(err, "get_user_quota_failed", http.StatusInternalServerError)
	}

	// 检查用户配额是否充足
	if userQuota <= 0 || userQuota-preConsumedQuota < 0 {
		// 用户配额不足，返回错误
		return 0, 0, service.OpenAIErrorWrapper(errors.New("user quota is not enough"), "insufficient_user_quota", http.StatusForbidden)
	}

	// 减少用户配额
	err = model.CacheDecreaseUserQuota(relayInfo.UserId, preConsumedQuota)
	if err != nil {
		// 减少用户配额失败，返回错误
		return 0, 0, service.OpenAIErrorWrapper(err, "decrease_user_quota_failed", http.StatusInternalServerError)
	}

	// 用户额度充足的情况下，检查令牌额度是否充足
	if userQuota > 100*preConsumedQuota {
		if !relayInfo.TokenUnlimited {
			// 令牌非无限，检查令牌额度
			tokenQuota := c.GetInt("token_quota")
			if tokenQuota > 100*preConsumedQuota {
				// 令牌额度充足，无需预消费
				preConsumedQuota = 0
				common.LogInfo(c.Request.Context(), fmt.Sprintf("user %d quota %d and token %d quota %d are enough, trusted and no need to pre-consume", relayInfo.UserId, userQuota, relayInfo.TokenId, tokenQuota))
			}
		} else {
			// 用户拥有无限令牌，配额充足，无需预消费
			preConsumedQuota = 0
			common.LogInfo(c.Request.Context(), fmt.Sprintf("user %d with unlimited token has enough quota %d, trusted and no need to pre-consume", relayInfo.UserId, userQuota))
		}
	}

	// 如果预消费配额大于0，尝试预先消费令牌配额
	if preConsumedQuota > 0 {
		userQuota, err = model.PreConsumeTokenQuota(relayInfo.TokenId, preConsumedQuota)
		if err != nil {
			// 预消费令牌配额失败，返回错误
			return 0, 0, service.OpenAIErrorWrapper(err, "pre_consume_token_quota_failed", http.StatusForbidden)
		}
	}

	// 返回预消费的配额数量和操作后的用户剩余配额
	return preConsumedQuota, userQuota, nil
}

// returnPreConsumedQuota 用于归还预先消费的配额
// 参数:
// c: gin上下文，用于传递请求相关的context
// tokenId: 令牌ID，标识特定的令牌
// userQuota: 用户配额，表示用户拥有的总配额量
// preConsumedQuota: 预先消费的配额量，需要归还的量
func returnPreConsumedQuota(c *gin.Context, tokenId int, userQuota int, preConsumedQuota int) {
	// 当预消费的配额不为0时，异步归还预消费的配额
	if preConsumedQuota != 0 {
		go func(ctx context.Context) {
			// 异步执行归还操作，将预消费的配额加回到用户配额中
			err := model.PostConsumeTokenQuota(tokenId, userQuota, -preConsumedQuota, 0, false)
			if err != nil {
				// 记录归还预消费配额失败的错误日志
				common.SysError("error return pre-consumed quota: " + err.Error())
			}
		}(c)
	}
}

func postConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, textRequest dto.GeneralOpenAIRequest,
	usage *dto.Usage, ratio float64, preConsumedQuota int, userQuota int, modelRatio float64, groupRatio float64,
	modelPrice float64, sensitiveResp *dto.SensitiveResponse) {

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	promptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens

	tokenName := ctx.GetString("token_name")

	quota := 0
	if modelPrice == -1 {
		completionRatio := common.GetCompletionRatio(textRequest.Model)
		quota = promptTokens + int(float64(completionTokens)*completionRatio)
		quota = int(float64(quota) * ratio)
		if ratio != 0 && quota <= 0 {
			quota = 1
		}
	} else {
		quota = int(modelPrice * common.QuotaPerUnit * groupRatio)
	}
	totalTokens := promptTokens + completionTokens
	var logContent string
	if modelPrice == -1 {
		logContent = fmt.Sprintf("模型倍率 %.2f，分组倍率 %.2f", modelRatio, groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += fmt.Sprintf("（可能是上游超时）")
		common.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, textRequest.Model, preConsumedQuota))
	} else {
		if sensitiveResp != nil {
			logContent += fmt.Sprintf("，敏感词：%s", strings.Join(sensitiveResp.SensitiveWords, ", "))
		}
		quotaDelta := quota - preConsumedQuota
		err := model.PostConsumeTokenQuota(relayInfo.TokenId, userQuota, quotaDelta, preConsumedQuota, true)
		if err != nil {
			common.LogError(ctx, "error consuming token remain quota: "+err.Error())
		}
		err = model.CacheUpdateUserQuota(relayInfo.UserId)
		if err != nil {
			common.LogError(ctx, "error update user quota cache: "+err.Error())
		}
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	logModel := textRequest.Model
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		logContent += fmt.Sprintf("，模型 %s", textRequest.Model)
	}
	model.RecordConsumeLog(ctx, relayInfo.UserId, relayInfo.ChannelId, promptTokens, completionTokens, logModel, tokenName, quota, logContent, relayInfo.TokenId, userQuota, int(useTimeSeconds), relayInfo.IsStream)

	//if quota != 0 {
	//
	//}
}
