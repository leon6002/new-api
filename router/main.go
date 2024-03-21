package router

import (
	"embed"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"one-api/common"
	"os"
	"strings"
)

// SetRouter 设置路由
// 参数:
// - router *gin.Engine: Gin框架的路由器实例
// - buildFS embed.FS: 用于提供前端静态文件的嵌入文件系统
// - indexPage []byte: 首页的字节切片，用于在服务器端渲染或重定向
func SetRouter(router *gin.Engine, buildFS embed.FS, indexPage []byte) {
	// 设置API路由
	SetApiRouter(router)
	// 设置仪表板路由
	SetDashboardRouter(router)
	// 设置中继路由
	SetRelayRouter(router)

	// 从环境变量获取前端基础URL
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")

	// 检查是否为master节点，并且是否设置了FRONTEND_BASE_URL
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = "" // 在master节点上忽略FRONTEND_BASE_URL
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}

	// 当未设置前端基础URL时，设置Web路由
	if frontendBaseUrl == "" {
		SetWebRouter(router, buildFS, indexPage)
	} else {
		// 移除前端基础URL的尾部斜杠
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		// 设置未匹配到任何路由时的处理，重定向到前端基础URL
		router.NoRoute(func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}
