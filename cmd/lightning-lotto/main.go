package main

import (
	"github.com/gin-gonic/gin"
	"github.com/waltersmuts/lightning-lotto/config"
	"github.com/waltersmuts/lightning-lotto/routes"
	"github.com/waltersmuts/lightning-lotto/state"
)

func main() {
	config.InitConfig()
	state := state.NewState()
	engine := gin.Default()
	routes.RegisterRoutes(engine, state)
	engine.Run(":8090")
}
