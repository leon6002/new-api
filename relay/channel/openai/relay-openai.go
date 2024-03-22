package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/http"
	"one-api/common"
	"one-api/constant"
	"one-api/dto"
	relayconstant "one-api/relay/constant"
	"one-api/service"
	"strings"
	"sync"
	"time"
)

// OpenaiStreamHandler 处理OpenAI的流式响应数据。
//
// 参数:
// c *gin.Context: Gin框架的上下文对象，用于HTTP请求的处理。
// resp *http.Response: HTTP响应对象，包含了从OpenAI获取的原始数据。
// relayMode int: 传递模式，决定如何处理和转发收到的数据。
//
// 返回值:
// *dto.OpenAIErrorWithStatusCode: 如果处理过程中遇到错误，返回包含错误信息和状态码的DTO。
// string: 处理后的响应文本，如果没有错误发生，将包含流式数据的聚合结果。
func OpenaiStreamHandler(c *gin.Context, resp *http.Response, relayMode int) (*dto.OpenAIErrorWithStatusCode, string) {
	// 检查是否需要对完成敏感词进行检查
	checkSensitive := constant.ShouldCheckCompletionSensitive()
	var responseTextBuilder strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	// 自定义分割逻辑，以换行符分隔响应体
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.Index(string(data), "\n"); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	dataChan := make(chan string, 5) // 数据通道，用于异步处理流数据
	stopChan := make(chan bool, 2)   // 停止通道，用于控制异步处理的结束
	defer close(stopChan)
	defer close(dataChan)
	var wg sync.WaitGroup
	go func() {
		wg.Add(1)
		defer wg.Done()
		var streamItems []string // 用于存储流数据项
		for scanner.Scan() {
			data := scanner.Text()
			if len(data) < 6 { // 忽略空白行或格式错误的数据
				continue
			}
			// 校验接收到的数据的格式，必须是data:开头或者[DONE]字符串
			if data[:6] != "data: " && data[:6] != "[DONE]" {
				continue
			}
			sensitive := false
			if checkSensitive {
				// 检查敏感词
				sensitive, _, data = service.SensitiveWordReplace(data, false)
			}
			// java项目中com.theokanning.openai.completion.chat.ChatMessage;设置了role为NonNull，
			// 如果这里role返回null的话在下游的java项目中会报错
			// 所以统一都替换为"assistant"
			data = strings.Replace(data, `"role":null`, `"role":"assistant"`, -1)

			dataChan <- data
			data = data[6:]
			if !strings.HasPrefix(data, "[DONE]") {
				streamItems = append(streamItems, data)
			}
			if sensitive && constant.StopOnSensitiveEnabled {
				dataChan <- "data: [DONE]"
				break
			}
		}
		streamResp := "[" + strings.Join(streamItems, ",") + "]"
		switch relayMode {
		case relayconstant.RelayModeChatCompletions:
			var streamResponses []dto.ChatCompletionsStreamResponseSimple
			err := json.Unmarshal(common.StringToByteSlice(streamResp), &streamResponses)
			if err != nil {
				common.SysError("error unmarshalling stream response: " + err.Error())
				return // 出错时忽略该错误
			}
			// 处理聊天完成的流响应
			for _, streamResponse := range streamResponses {
				for _, choice := range streamResponse.Choices {
					responseTextBuilder.WriteString(choice.Delta.Content)
				}
			}
		case relayconstant.RelayModeCompletions:
			var streamResponses []dto.CompletionsStreamResponse
			err := json.Unmarshal(common.StringToByteSlice(streamResp), &streamResponses)
			if err != nil {
				common.SysError("error unmarshalling stream response: " + err.Error())
				return // 出错时忽略该错误
			}
			// 处理完成的流响应
			for _, streamResponse := range streamResponses {
				for _, choice := range streamResponse.Choices {
					responseTextBuilder.WriteString(choice.Text)
				}
			}
		}
		if len(dataChan) > 0 {
			// 等待数据耗尽
			time.Sleep(2 * time.Second)
		}
		common.SafeSend(stopChan, true)
	}()
	service.SetEventStreamHeaders(c) // 设置事件流的HTTP头
	c.Stream(func(w io.Writer) bool {
		select {
		case data := <-dataChan:
			if strings.HasPrefix(data, "data: [DONE]") {
				data = data[:12]
			}
			// 移除数据末尾可能的\r字符
			data = strings.TrimSuffix(data, "\r")
			c.Render(-1, common.CustomEvent{Data: data}) // 渲染并发送数据
			return true
		case <-stopChan:
			return false
		}
	})
	err := resp.Body.Close()
	if err != nil {
		return service.OpenAIErrorWrapper(err, "close_response_body_failed", http.StatusInternalServerError), ""
	}
	wg.Wait()
	return nil, responseTextBuilder.String() // 返回处理后的响应文本
}

func OpenaiHandler(c *gin.Context, resp *http.Response, promptTokens int, model string) (*dto.OpenAIErrorWithStatusCode, *dto.Usage, *dto.SensitiveResponse) {
	var textResponse dto.TextResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError), nil, nil
	}
	err = resp.Body.Close()
	if err != nil {
		return service.OpenAIErrorWrapper(err, "close_response_body_failed", http.StatusInternalServerError), nil, nil
	}
	err = json.Unmarshal(responseBody, &textResponse)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "unmarshal_response_body_failed", http.StatusInternalServerError), nil, nil
	}
	log.Printf("textResponse: %+v", textResponse)
	if textResponse.Error != nil {
		return &dto.OpenAIErrorWithStatusCode{
			Error:      *textResponse.Error,
			StatusCode: resp.StatusCode,
		}, nil, nil
	}

	checkSensitive := constant.ShouldCheckCompletionSensitive()
	sensitiveWords := make([]string, 0)
	triggerSensitive := false

	if textResponse.Usage.TotalTokens == 0 || checkSensitive {
		completionTokens := 0
		for _, choice := range textResponse.Choices {
			stringContent := string(choice.Message.Content)
			ctkm, _, _ := service.CountTokenText(stringContent, model, false)
			completionTokens += ctkm
			if checkSensitive {
				sensitive, words, stringContent := service.SensitiveWordReplace(stringContent, false)
				if sensitive {
					triggerSensitive = true
					msg := choice.Message
					msg.Content = common.StringToByteSlice(stringContent)
					choice.Message = msg
					sensitiveWords = append(sensitiveWords, words...)
				}
			}
		}
		textResponse.Usage = dto.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		}
	}

	if constant.StopOnSensitiveEnabled {

	} else {
		responseBody, err = json.Marshal(textResponse)
		// Reset response body
		resp.Body = io.NopCloser(bytes.NewBuffer(responseBody))
		// We shouldn't set the header before we parse the response body, because the parse part may fail.
		// And then we will have to send an error response, but in this case, the header has already been set.
		// So the httpClient will be confused by the response.
		// For example, Postman will report error, and we cannot check the response at all.
		for k, v := range resp.Header {
			c.Writer.Header().Set(k, v[0])
		}
		c.Writer.WriteHeader(resp.StatusCode)
		_, err = io.Copy(c.Writer, resp.Body)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError), nil, nil
		}
		err = resp.Body.Close()
		if err != nil {
			return service.OpenAIErrorWrapper(err, "close_response_body_failed", http.StatusInternalServerError), nil, nil
		}
	}

	if checkSensitive && triggerSensitive {
		sensitiveWords = common.RemoveDuplicate(sensitiveWords)
		return service.OpenAIErrorWrapper(errors.New(fmt.Sprintf("sensitive words detected: %s", strings.Join(sensitiveWords, ", "))), "sensitive_words_detected", http.StatusBadRequest), &textResponse.Usage, &dto.SensitiveResponse{
			SensitiveWords: sensitiveWords,
		}
	}
	return nil, &textResponse.Usage, nil
}
