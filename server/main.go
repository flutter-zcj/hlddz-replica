// 斗地主牌局服务入口。基于 cherry 框架装配网关组件。
package main

import (
	"os"

	cherry "github.com/cherry-game/cherry"

	"ddz/gateway"
)

func main() {
	addr := os.Getenv("DDZ_ADDR")
	if addr == "" {
		addr = ":9104"
	}
	profilePath := os.Getenv("DDZ_PROFILE")
	if profilePath == "" {
		profilePath = "etc/profile/dev.json"
	}
	nodeID := os.Getenv("DDZ_NODE")
	if nodeID == "" {
		nodeID = "ddz-1"
	}
	app := cherry.Configure(profilePath, nodeID, true, cherry.Standalone)
	app.Register(gateway.NewGateway(addr))
	app.Startup()
}
