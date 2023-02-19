package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	qrcode "github.com/skip2/go-qrcode"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Ticket struct {
	NodeID     string
	AmountSats uint64
}

type countdownTimer struct {
	ticker   time.Ticker
	lastTick time.Time
}

func (t *Ticket) String() string {
	return fmt.Sprintf("%s:%d\n", t.NodeID, t.AmountSats)
}

type state struct {
	tickets   []*Ticket
	countdown countdownTimer
	mu        sync.RWMutex
}

func (n *state) addTicketRequest(c *gin.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()

	nodeID := c.Request.URL.Query().Get("node_id")
	amountSatsString := c.Request.URL.Query().Get("amount")
	amountSats, err := strconv.ParseInt(amountSatsString, 10, 64)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	n.tickets = append(n.tickets, &Ticket{nodeID, uint64(amountSats)})

	result := "<!DOCTYPE html> <html>"
	result += n.printStateUnsafe()

	file, err := ioutil.ReadFile("client_poll.js")
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
	}
	result += fmt.Sprintf("<script>  %s </script> ", string(file))
	result += fmt.Sprintf("</html>")
	c.String(http.StatusOK, result)
}

func (n *state) handlePollInvoiceRequest(c *gin.Context) {
	fmt.Println("Received connection")
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Fprintf(c.Writer, "ERROR %v", err)
		return
	}
	n.handlePollInvoiceWs(ws)
	fmt.Println("Written")
}

func (n *state) handlePollInvoiceWs(ws *websocket.Conn) {
	time.Sleep(2 * time.Second)
	fmt.Println("Writing...")
	err := ws.WriteMessage(websocket.TextMessage, []byte("Paid"))
	if err != nil {
		fmt.Printf("ERR %v\n", err)
		return
	}
}

func handleInvoiceQR(c *gin.Context) {
	png, err := qrcode.Encode("https://example.org", qrcode.Medium, 256)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	c.Header("Content-Type", "image/png")
	c.Writer.Write(png)
}

func (n *state) printTickets(c *gin.Context) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	c.HTML(http.StatusOK, "index.html", gin.H{"payload": n.tickets})
}

func (n *state) printStateUnsafe() string {
	tenSeconds := 10 * time.Second
	result := fmt.Sprintf("Time left in seconds: %f", (tenSeconds - time.Now().Sub(n.countdown.lastTick)).Seconds())
	for _, t := range n.tickets {
		result += t.String()
	}
	return result
}

func main() {
	var s state
	countdown := countdownTimer{*time.NewTicker(10 * time.Second), time.Now()}
	s.countdown = countdown
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-s.countdown.ticker.C:
				fmt.Println("Cleared at", t)
				s.countdown.lastTick = time.Now()
				s.tickets = nil
			}
		}
	}()

	r := gin.Default()
	r.LoadHTMLGlob("*.html")
	r.GET("/", s.printTickets)
	r.GET("/add_ticket_request", s.addTicketRequest)
	r.GET("/invoice_qr", handleInvoiceQR)
	r.GET("/poll_invoice", s.handlePollInvoiceRequest)
	r.Run(":8090")
}
