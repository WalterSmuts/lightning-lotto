package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"

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

type Winner struct {
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
	tickets        []*Ticket
	winners        []*Winner
	countdown      countdownTimer
	mu             sync.RWMutex
	pot            uint64
	lnd            lndclient.LightningClient
	invoice_client lndclient.InvoicesClient
}

func (n *state) addTicketRequest(c *gin.Context) {
	nodeID := c.Request.URL.Query().Get("node_id")
	amountSatsString := c.Request.URL.Query().Get("amount")
	amountSats, err := strconv.ParseInt(amountSatsString, 10, 64)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	tenSeconds := 10 * time.Second
	time_left := (tenSeconds - time.Now().Sub(n.countdown.lastTick)).Seconds()

	hash, invoice, err := n.lnd.AddInvoice(c.Request.Context(), &invoicesrpc.AddInvoiceData{
		Memo:            "lightning-lotto",
		Value:           lnwire.MilliSatoshi(amountSats * 1000),
		DescriptionHash: nil,
	})

	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}
	update_chan, err_chan, err := n.invoice_client.SubscribeSingleInvoice(context.Background(), hash)
	go func() {
		for {
			select {
			case update := <-update_chan:
				fmt.Printf("Payment %v has changed to state %v\n", hash, update)
				if update.State == channeldb.ContractSettled {
					n.addTicket(&Ticket{nodeID, uint64(amountSats)})
					return
				}
			case err := <-err_chan:
				fmt.Printf("ERROR %v\n", err)

			}
		}
	}()

	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
		return
	}

	c.HTML(http.StatusPaymentRequired, "add_ticket_request.html", gin.H{"time_left": time_left, "invoice": invoice, "hash": hash})
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
	totalNumberOfTickets := 0
	for _, ticket := range n.tickets {
		totalNumberOfTickets += int(ticket.AmountSats)
	}

	if totalNumberOfTickets > 0 {
		selected := rand.Intn(totalNumberOfTickets)
		ticketSum := 0

		previousTicketSum := 0
		selected_node_id := "Unknown"
		for _, ticket := range n.tickets {
			ticketSum += int(ticket.AmountSats)
			if previousTicketSum <= selected && ticketSum > selected {
				selected_node_id = ticket.NodeID
				break
			}
			previousTicketSum = ticketSum
		}
		n.winners = append(n.winners, &Winner{selected_node_id, n.pot})
	}

	n.tickets = nil
	n.pot = 0
	n.countdown.lastTick = time.Now()
}

func (n *state) handlePollInvoiceRequest(c *gin.Context) {
	hash_str := c.Request.URL.Query().Get("hash")
	hash, err := lntypes.MakeHashFromStr(hash_str)
	if err != nil {
		fmt.Fprintf(c.Writer, "ERROR %v", err)
		return
	}

	fmt.Println("Received connection")
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Fprintf(c.Writer, "ERROR %v", err)
		return
	}
	n.handlePollInvoiceWs(ws, hash)
	fmt.Println("Written")
}

func (n *state) handlePollInvoiceWs(ws *websocket.Conn, hash lntypes.Hash) {
	update_chan, err_chan, err := n.invoice_client.SubscribeSingleInvoice(context.Background(), hash)
	if err != nil {
		fmt.Printf("ERROR %v", err)
		return
	}
	for {
		select {
		case update := <-update_chan:
			fmt.Printf("Payment %v has changed to state %v\n", hash, update)
			if update.State == channeldb.ContractSettled {
				err = ws.WriteMessage(websocket.TextMessage, []byte("Paid"))
				if err != nil {
					fmt.Printf("ERR %v\n", err)
					return
				}

				return
			}
		case err := <-err_chan:
			fmt.Printf("ERROR %v\n", err)
		}
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
	tenSeconds := 10 * time.Second
	time_left := (tenSeconds - time.Now().Sub(n.countdown.lastTick)).Seconds()
	c.HTML(http.StatusOK, "index.html", gin.H{"payload": n.tickets, "pot": n.pot, "time_left": time_left, "winners": n.winners})
}

func main() {
	lnd, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:  "localhost",
		Network:     "mainnet",
		MacaroonDir: "/home/walter/.lnd/data/chain/bitcoin/mainnet",
		TLSPath:     "/home/walter/.lnd/tls.cert",
	})
	if err != nil {
		fmt.Printf("%v", err)
		return
	}

	var s state
	s.lnd = lnd.Client
	s.invoice_client = lnd.Invoices
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
