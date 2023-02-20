package main

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	qrcode "github.com/skip2/go-qrcode"
)

const TEST_INVOICE = "lnbc100n1p3lxkw5pp5lvzkgwnvvl8tv9z3j9ssp7ntreh79tp0f2nds4dc5sy5rsm6295sdqcd35kw6r5de5kueedd3hhgar0cqzpgxqyz5vqsp5rmwx8dt2q4p8kcz4lxz30p3tc54ja9qgp5l5adpzazg30wjl0snq9qyyssq33waacw3z96u643ncagcgefluzp3d0fdtr8trf77yl7s2akp89asyxnqvy4rcuheznneat4mlyeuejc30q0f2s5fllj3nqkwr2wx35sq9ltdwt"

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
	pot       uint64
}

func (n *state) addTicketRequest(c *gin.Context) {
	nodeID := c.Request.URL.Query().Get("node_id")
	amountSatsString := c.Request.URL.Query().Get("amount")
	amountSats, err := strconv.ParseInt(amountSatsString, 10, 64)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	n.addTicket(&Ticket{nodeID, uint64(amountSats)})

	tenSeconds := 10 * time.Second
	time_left := (tenSeconds - time.Now().Sub(n.countdown.lastTick)).Seconds()
	invoice := TEST_INVOICE
	c.HTML(http.StatusOK, "add_ticket_request.html", gin.H{"time_left": time_left, "invoice": invoice})
}

func (n *state) addTicket(ticket *Ticket) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tickets = append(n.tickets, ticket)
	n.pot += ticket.AmountSats
}

func (n *state) reset() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.tickets = nil
	n.pot = 0
	n.countdown.lastTick = time.Now()
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
	time.Sleep(5 * time.Second)
	fmt.Println("Writing...")
	err := ws.WriteMessage(websocket.TextMessage, []byte("Paid"))
	if err != nil {
		fmt.Printf("ERR %v\n", err)
		return
	}
}

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

func (n *state) printTickets(c *gin.Context) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	c.HTML(http.StatusOK, "index.html", gin.H{"payload": n.tickets})
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
				s.reset()
			}
		}
	}()

	r := gin.Default()
	r.LoadHTMLGlob("*.html")
	r.GET("/", s.printTickets)
	r.GET("/add_ticket_request", s.addTicketRequest)
	r.GET("/invoice_qr", handleInvoiceQR)
	r.GET("/ws/poll_invoice", s.handlePollInvoiceRequest)
	r.Run(":8090")
}
