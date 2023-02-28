package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/waltersmuts/lightning-lotto/config"
	"github.com/waltersmuts/lightning-lotto/routes"
	"github.com/waltersmuts/lightning-lotto/state"
)

func handleInvoiceQR(c *gin.Context) {
	invoice := c.Request.URL.Query().Get("invoice")
	png, err := qrcode.Encode(invoice, qrcode.Medium, 256)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	c.Header("Content-Type", "image/png")
	c.Writer.Write(png)
}

func main() {
	config.InitConfig()
	s := state.NewState()
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-s.CountdownTimerChannel():
				fmt.Println("Cleared at", t)
				s.Reset()
			}
		}
	}()

	r := gin.Default()
	r.LoadHTMLGlob("static/*.html")
	r.GET("/", routes.PrintTickets(s))
	r.GET("/add_ticket_request", routes.AddTicketRequest(s))
	r.GET("/invoice_qr", handleInvoiceQR)
	r.GET("/ws/poll_invoice", routes.HandlePollInvoiceRequest(s))
	r.GET("/ws/stream_tickets", routes.HandleStreamTicketsWs(s))
	r.Run(":8090")
}
