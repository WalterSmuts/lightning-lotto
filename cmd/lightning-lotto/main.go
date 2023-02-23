package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
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
	r.LoadHTMLGlob("*.html")
	r.GET("/", s.PrintTickets)
	r.GET("/add_ticket_request", s.AddTicketRequest)
	r.GET("/invoice_qr", handleInvoiceQR)
	r.GET("/ws/poll_invoice", s.HandlePollInvoiceRequest)
	r.Run(":8090")
}
