package routes

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"

	"github.com/waltersmuts/lightning-lotto/state"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func AddTicketRequest(n *state.State) func(c *gin.Context) {
	return func(c *gin.Context) {
		nodeID := c.Request.URL.Query().Get("node_id")
		amountSatsString := c.Request.URL.Query().Get("amount")
		amountSats, err := strconv.ParseUint(amountSatsString, 10, 64)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
			return
		}

		timeLeft := n.Countdown.TimeLeft()

		hash, invoice, err := n.Lnd.AddInvoice(c.Request.Context(), &invoicesrpc.AddInvoiceData{
			Memo:            "lightning-lotto",
			Value:           lnwire.MilliSatoshi(amountSats * 1000),
			DescriptionHash: nil,
		})

		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
			return
		}
		updateChan, errChan, err := n.Invoice_client.SubscribeSingleInvoice(context.Background(), hash)
		go func() {
			for {
				select {
				case update := <-updateChan:
					fmt.Printf("Payment %v has changed to state %v\n", hash, update)
					if update.State == channeldb.ContractSettled {
						n.AddTicket(&state.Ticket{NodeID: nodeID, AmountSats: amountSats})
						return
					}
				case err := <-errChan:
					fmt.Printf("ERROR %v\n", err)

				}
			}
		}()

		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("ERROR %v", err))
			return
		}

		c.HTML(http.StatusPaymentRequired, "add_ticket_request.html", gin.H{"time_left": timeLeft, "invoice": invoice, "hash": hash})
	}
}

func HandlePollInvoiceRequest(n *state.State) func(c *gin.Context) {
	return func(c *gin.Context) {
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
		n.HandlePollInvoiceWs(ws, hash)
		fmt.Println("Written")
	}
}

func HandleStreamTicketsWs(n *state.State) func(c *gin.Context) {
	return func(c *gin.Context) {
		fmt.Println("Received connection")
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			fmt.Printf("ERROR %v", err)
			return
		}
		n.HandleStreamTicketsWs(ws)
	}
}

func PrintTickets(n *state.State) func(c *gin.Context) {
	return func(c *gin.Context) {
		displayState := n.ReadDisplayState()
		c.HTML(http.StatusOK, "index.html", gin.H{"payload": displayState.Tickets, "pot": displayState.Pot, "time_left_ms": displayState.TimeLeft.Milliseconds(), "winners": displayState.Winners})
	}
}
