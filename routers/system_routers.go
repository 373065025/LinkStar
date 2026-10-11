package routers

import (
	"linkstar/api"
	"linkstar/api/system_api"
	"linkstar/middleware"

	"github.com/gin-gonic/gin"
)

func SystemRouters(g *gin.RouterGroup) {
	var app = api.App.SystemApi

	// 公开：获取版本号
	g.GET("version", app.GetVersion)
}

func SystemRoutersProtected(g *gin.RouterGroup) {
	var app = api.App.SystemApi

	// 运行日志：日志里有内网地址、域名、后端错误，不能公开
	g.GET("system/log/days", app.LogDaysView)
	g.GET("system/log", app.LogListView)

	// 一键更新：换二进制、重启进程，必须登录
	g.GET("system/update", app.UpdateStatusView)
	g.POST("system/update/check", app.UpdateCheckView)
	g.POST("system/update/install", app.UpdateInstallView)
	g.POST("system/update/upload", app.UpdateUploadView)
	g.PUT(
		"system/update/config",
		middleware.BindJsonMiddleware[system_api.UpdateConfigRequest],
		app.UpdateConfigView,
	)
}
