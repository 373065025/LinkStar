package system_api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Version 当前版本号。是 var 为了能在编译时覆盖、不改源码编测试包：
// go build -ldflags "-X linkstar/api/system_api.Version=0.6.9"
var Version = "0.7.1"

type VersionResponse struct {
	Version string `json:"version"`
}

func (SystemApi) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": VersionResponse{
			Version: Version,
		},
	})
}
