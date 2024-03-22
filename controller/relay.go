package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"one-api/common"
	"one-api/dto"
	"one-api/relay"
	"one-api/relay/constant"
	relayconstant "one-api/relay/constant"
	"one-api/service"
	"strconv"
)

// Relay 是一个处理中继请求的函数。
// 它根据请求的URL路径来决定是处理图像生成、音频处理还是文本处理，并在处理过程中进行错误处理和重试逻辑。
//
// 参数:
// - c *gin.Context: Gin框架的上下文对象，用于处理HTTP请求和响应。
func Relay(c *gin.Context) {
	// 根据请求URL的路径，确定中继模式。
	relayMode := constant.Path2RelayMode(c.Request.URL.Path)
	var err *dto.OpenAIErrorWithStatusCode
	switch relayMode {
	case relayconstant.RelayModeImagesGenerations:
		// 处理图像生成的请求。
		err = relay.RelayImageHelper(c, relayMode)
	case relayconstant.RelayModeAudioSpeech:
		// 处理音频转文本的请求，此模式下会自动继续处理音频翻译和转录。
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		// 音频处理的通用逻辑。
		err = relay.AudioHelper(c, relayMode)
	default:
		// 默认处理文本相关的请求。
		err = relay.TextHelper(c)
	}
	if err != nil {
		// 错误处理逻辑。
		requestId := c.GetString(common.RequestIdKey)
		retryTimesStr := c.Query("retry")
		retryTimes, _ := strconv.Atoi(retryTimesStr)
		if retryTimesStr == "" {
			retryTimes = common.RetryTimes
		}
		// 实施重试逻辑。
		if retryTimes > 0 {
			c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s?retry=%d", c.Request.URL.Path, retryTimes-1))
		} else {
			// 当请求被限制过多时，进行特定处理。
			if err.StatusCode == http.StatusTooManyRequests {
				// 请求过多的处理逻辑。
			}
			// 错误响应格式化。
			err.Error.Message = common.MessageWithRequestId(err.Error.Message, requestId)
			c.JSON(err.StatusCode, gin.H{
				"error": err.Error,
			})
		}
		channelId := c.GetInt("channel_id")
		autoBan := c.GetBool("auto_ban")
		// 记录错误日志，并在特定条件下禁用频道。
		common.LogError(c.Request.Context(), fmt.Sprintf("relay error (channel #%d): %s", channelId, err.Error.Message))
		if service.ShouldDisableChannel(&err.Error, err.StatusCode) && autoBan {
			service.DisableChannel(channelId, c.GetString("channel_name"), err.Error.Message)
		}
	}
}

// RelayMidjourney 处理中继的中途任务。
// 根据请求中携带的"relay_mode"参数决定调用哪个具体的中继辅助函数。
// 参数 c 是Gin框架的上下文对象，用于处理HTTP请求和响应。
func RelayMidjourney(c *gin.Context) {
	relayMode := c.GetInt("relay_mode")
	var err *dto.MidjourneyResponse
	switch relayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		err = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		err = relay.RelayMidjourneyTask(c, relayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		err = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		err = relay.RelaySwapFace(c)
	default:
		err = relay.RelayMidjourneySubmit(c, relayMode)
	}
	log.Println(err)
	if err != nil {
		statusCode := http.StatusBadRequest
		if err.Code == 30 {
			err.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", err.Description, err.Result),
			"type":        "upstream_error",
			"code":        err.Code,
		})
		channelId := c.GetInt("channel_id")
		common.SysError(fmt.Sprintf("relay error (channel #%d): %s", channelId, fmt.Sprintf("%s %s", err.Description, err.Result)))
	}
}

// RelayNotImplemented 处理未实现的API请求。
// 返回一个提示信息，说明API尚未实现。
// 参数 c 是Gin框架的上下文对象，用于返回HTTP响应。
func RelayNotImplemented(c *gin.Context) {
	err := dto.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

// RelayNotFound 处理无效URL的请求。
// 返回一个错误信息，指出请求的URL无效。
// 参数 c 是Gin框架的上下文对象，用于返回HTTP响应。
func RelayNotFound(c *gin.Context) {
	err := dto.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}
