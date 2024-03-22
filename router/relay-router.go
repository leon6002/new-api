package router

import (
	"github.com/gin-gonic/gin"
	"one-api/controller"
	"one-api/middleware"
	"one-api/relay"
)

// SetRelayRouter 设置Relay路由器
// 参数:
// - router: 指向Gin引擎的指针，用于设置路由
func SetRelayRouter(router *gin.Engine) {
	// 全局中间件：跨域资源共享(CORS)
	router.Use(middleware.CORS())

	// V1模型路由组，使用Token认证
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.TokenAuth())
	{
		// 列出所有模型
		modelsRouter.GET("", controller.ListModels)
		// 获取指定模型的信息
		modelsRouter.GET("/:model", controller.RetrieveModel)
	}

	// V1 Relay路由组，使用Token认证和分布中间件
	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		// 一系列Relay处理函数，用于不同类型的请求
		relayV1Router.POST("/completions", controller.Relay)
		relayV1Router.POST("/chat/completions", controller.Relay)
		relayV1Router.POST("/edits", controller.Relay)
		relayV1Router.POST("/images/generations", controller.Relay)
		relayV1Router.POST("/images/edits", controller.RelayNotImplemented)
		relayV1Router.POST("/images/variations", controller.RelayNotImplemented)
		relayV1Router.POST("/embeddings", controller.Relay)
		relayV1Router.POST("/engines/:model/embeddings", controller.Relay)
		relayV1Router.POST("/audio/transcriptions", controller.Relay)
		relayV1Router.POST("/audio/translations", controller.Relay)
		relayV1Router.POST("/audio/speech", controller.Relay)
		relayV1Router.GET("/files", controller.RelayNotImplemented)
		relayV1Router.POST("/files", controller.RelayNotImplemented)
		relayV1Router.DELETE("/files/:id", controller.RelayNotImplemented)
		relayV1Router.GET("/files/:id", controller.RelayNotImplemented)
		relayV1Router.GET("/files/:id/content", controller.RelayNotImplemented)
		relayV1Router.POST("/fine-tunes", controller.RelayNotImplemented)
		relayV1Router.GET("/fine-tunes", controller.RelayNotImplemented)
		relayV1Router.GET("/fine-tunes/:id", controller.RelayNotImplemented)
		relayV1Router.POST("/fine-tunes/:id/cancel", controller.RelayNotImplemented)
		relayV1Router.GET("/fine-tunes/:id/events", controller.RelayNotImplemented)
		relayV1Router.DELETE("/models/:model", controller.RelayNotImplemented)
		relayV1Router.POST("/moderations", controller.Relay)
	}

	// MJ路由组，用于Midjourney相关的请求，使用Token认证和分布中间件
	relayMjRouter := router.Group("/mj")
	relayMjRouter.GET("/image/:id", relay.RelayMidjourneyImage)
	relayMjRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		// Midjourney的各种提交操作
		relayMjRouter.POST("/submit/action", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/shorten", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/modal", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/imagine", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/change", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/simple-change", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/describe", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/blend", controller.RelayMidjourney)
		relayMjRouter.POST("/notify", controller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/fetch", controller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/image-seed", controller.RelayMidjourney)
		relayMjRouter.POST("/task/list-by-condition", controller.RelayMidjourney)
		relayMjRouter.POST("/insight-face/swap", controller.RelayMidjourney)
	}
	// 注释掉的Use调用，可能是预留的中间件配置位置
	//relayMjRouter.Use()
}
