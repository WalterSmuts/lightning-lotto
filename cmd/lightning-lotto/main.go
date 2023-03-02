package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/waltersmuts/lightning-lotto/config"
	"github.com/waltersmuts/lightning-lotto/routes"
	"github.com/waltersmuts/lightning-lotto/state"
)

func main() {
	config.InitConfig()
	state := state.NewState()
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-state.CountdownTimerChannel():
				fmt.Println("Cleared at", t)
				state.Reset()
			}
		}
	}()

	engine := gin.Default()
	routes.RegisterRoutes(engine, state)
	engine.Run(":8090")
}
