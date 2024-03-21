package common

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
)

// UnmarshalBodyReusable 用于解析请求体，并且允许请求体在解析后能够被再次使用。
// 参数:
// - c *gin.Context: Gin框架的上下文对象，用于访问HTTP请求和其他相关数据。
// - v any: 用于存储解析后的JSON数据的变量，其类型可为任意支持JSON解码的类型。
// 返回值:
// - error: 如果在读取请求体、关闭请求体或解析JSON过程中发生错误，则返回相应的错误信息；否则返回nil。
func UnmarshalBodyReusable(c *gin.Context, v any) error {
	// 读取请求体
	requestBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}

	// 关闭请求体，释放资源
	err = c.Request.Body.Close()
	if err != nil {
		return err
	}

	// 解析JSON数据到v变量中
	err = json.Unmarshal(requestBody, &v)
	if err != nil {
		return err
	}

	// 重置请求体，以便于再次使用
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	return nil
}
