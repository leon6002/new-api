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

// Relay 处理中继请求。
// 根据请求的URL路径来决定调用哪个具体的中继辅助函数。
// 参数 c 是Gin框架的上下文对象，用于处理HTTP请求和响应。
func Relay(c *gin.Context) {
	relayMode := constant.Path2RelayMode(c.Request.URL.Path)
	var err *dto.OpenAIErrorWithStatusCode
	switch relayMode {
	case relayconstant.RelayModeImagesGenerations:
		err = relay.RelayImageHelper(c, relayMode)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, relayMode)
	default:
		err = relay.TextHelper(c)
	}
	if err != nil {
		requestId := c.GetString(common.RequestIdKey)
		retryTimesStr := c.Query("retry")
		retryTimes, _ := strconv.Atoi(retryTimesStr)
		if retryTimesStr == "" {
			retryTimes = common.RetryTimes
		}
		if retryTimes > 0 {
			c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s?retry=%d", c.Request.URL.Path, retryTimes-1))
		} else {
			if err.StatusCode == http.StatusTooManyRequests {
				// 当请求状态码为429时，表示请求过多，可以在此处添加特定处理逻辑
			}
			err.Error.Message = common.MessageWithRequestId(err.Error.Message, requestId)
			c.JSON(err.StatusCode, gin.H{
				"error": err.Error,
			})
		}
		channelId := c.GetInt("channel_id")
		autoBan := c.GetBool("auto_ban")
		common.LogError(c.Request.Context(), fmt.Sprintf("relay error (channel #%d): %s", channelId, err.Error.Message))
		if service.ShouldDisableChannel(&err.Error, err.StatusCode) && autoBan {
			channelId := c.GetInt("channel_id")
			channelName := c.GetString("channel_name")
			service.DisableChannel(channelId, channelName, err.Error.Message)
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
