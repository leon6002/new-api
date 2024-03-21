package main

import (
	"embed"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"one-api/common"
	"one-api/controller"
	"one-api/middleware"
	"one-api/model"
	"one-api/router"
	"one-api/service"
	"os"
	"strconv"

	_ "net/http/pprof"
)

//go:embed web/build
var buildFS embed.FS

//go:embed web/build/index.html
var indexPage []byte

// 主函数：初始化并启动API服务
func main() {
	// 设置日志配置
	common.SetupLogger()
	// 记录启动日志
	common.SysLog("New API " + common.Version + " started")
	// 根据环境变量设置Gin运行模式
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	// 如果开启调试模式，记录日志
	if common.DebugEnabled {
		common.SysLog("running in debug mode")
	}
	// 初始化SQL数据库
	err := model.InitDB()
	if err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
	}
	// 确保程序退出时关闭数据库连接
	defer func() {
		err := model.CloseDB()
		if err != nil {
			common.FatalLog("failed to close database: " + err.Error())
		}
	}()

	// 初始化Redis连接
	err = common.InitRedisClient()
	if err != nil {
		common.FatalLog("failed to initialize Redis: " + err.Error())
	}

	// 初始化配置选项
	model.InitOptionMap()
	// 兼容旧版本设置
	if common.RedisEnabled {
		common.MemoryCacheEnabled = true
	}
	// 如果开启内存缓存，进行相关初始化
	if common.MemoryCacheEnabled {
		common.SysLog("memory cache enabled")
		common.SysError(fmt.Sprintf("sync frequency: %d seconds", common.SyncFrequency))
		model.InitChannelCache()
	}
	// 如果开启Redis缓存，启动定时同步令牌缓存任务
	if common.RedisEnabled {
		go model.SyncTokenCache(common.SyncFrequency)
	}
	// 如果开启内存缓存，启动定时同步选项和频道缓存任务
	if common.MemoryCacheEnabled {
		go model.SyncOptions(common.SyncFrequency)
		go model.SyncChannelCache(common.SyncFrequency)
	}

	// 启动数据看板更新任务
	go model.UpdateQuotaData()

	// 根据环境变量配置自动更新和测试频道的频率
	if os.Getenv("CHANNEL_UPDATE_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_UPDATE_FREQUENCY"))
		if err != nil {
			common.FatalLog("failed to parse CHANNEL_UPDATE_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyUpdateChannels(frequency)
	}
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err != nil {
			common.FatalLog("failed to parse CHANNEL_TEST_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyTestChannels(frequency)
	}
	// 安全启动更新中转任务
	common.SafeGoroutine(func() {
		controller.UpdateMidjourneyTaskBulk()
	})
	// 根据环境变量开启批量更新功能
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		common.BatchUpdateEnabled = true
		common.SysLog("batch update enabled with interval " + strconv.Itoa(common.BatchUpdateInterval) + "s")
		model.InitBatchUpdater()
	}

	// 如果开启pprof，启动相关监听并记录日志
	if os.Getenv("ENABLE_PPROF") == "true" {
		go func() {
			log.Println(http.ListenAndServe("0.0.0.0:8005", nil))
		}()
		go common.Monitor()
		common.SysLog("pprof enabled")
	}

	// 初始化令牌编码器
	service.InitTokenEncoders()

	// 初始化HTTP服务器，配置恢复中间件、请求ID中间件、日志中间件和会话中间件
	server := gin.New()
	server.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		common.SysError(fmt.Sprintf("panic detected: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Panic detected, error: %v. Please submit a issue here: https://github.com/Calcium-Ion/new-api", err),
				"type":    "new_api_panic",
			},
		})
	}))
	server.Use(middleware.RequestId())
	middleware.SetUpLogger(server)
	// 初始化会话存储
	store := cookie.NewStore([]byte(common.SessionSecret))
	server.Use(sessions.Sessions("session", store))

	// 设置路由
	router.SetRouter(server, buildFS, indexPage)
	// 根据环境变量或默认配置启动服务器
	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}
	err = server.Run(":" + port)
	if err != nil {
		common.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
