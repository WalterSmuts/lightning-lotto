package state

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (n *State) AddTicketRequest(c *gin.Context) {
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

func (n *State) HandlePollInvoiceRequest(c *gin.Context) {
	hash_str := c.Request.URL.Query().Get("hash")
	hash, err := lntypes.MakeHashFromStr(hash_str)
	if err != nil {
		fmt.Fprintf(c.Writer, "ERROR %v", err)
		return
	}

	fmt.Println("Received connection")
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Printf("ERROR %v", err)
		return
	}
	n.handlePollInvoiceWs(ws, hash)
	fmt.Println("Written")
}

func (n *State) PrintTickets(c *gin.Context) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	tenSeconds := 10 * time.Second
	time_left := (tenSeconds - time.Now().Sub(n.countdown.lastTick)).Seconds()
	c.HTML(http.StatusOK, "index.html", gin.H{"payload": n.tickets, "pot": n.pot, "time_left": time_left, "winners": n.winners})
}
