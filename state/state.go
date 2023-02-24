package state

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/btcsuite/btcutil"
	"github.com/gorilla/websocket"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/routing/route"
)

const myNodeID string = "03ef68ccd4b33ae540aea5bf91fcfc70137e031e0cf3823a958c3c3d69239eb7cd"

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
	duration time.Duration
}

func (t countdownTimer) timeLeft() time.Duration {
	time_left := (t.duration - time.Now().Sub(t.lastTick))
	return time_left
}

func newCountDownTimer(duration time.Duration) countdownTimer {
	return countdownTimer{*time.NewTicker(duration), time.Now(), duration}
}

func (t *Ticket) String() string {
	return fmt.Sprintf("%s:%d\n", t.NodeID, t.AmountSats)
}

type State struct {
	tickets        []*Ticket
	winners        []*Winner
	countdown      countdownTimer
	mu             sync.RWMutex
	pot            uint64
	lnd            lndclient.LightningClient
	invoice_client lndclient.InvoicesClient
	router         lndclient.RouterClient
}

func (n *State) getPayoutSize() uint64 {
	return uint64(float64(n.pot) * 0.99)
}

type displayState struct {
	tickets  []*Ticket
	winners  []*Winner
	timeLeft time.Duration
	pot      uint64
}

func NewState() *State {
	var s State
	lnd, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:  "localhost",
		Network:     "mainnet",
		MacaroonDir: "/home/walter/.lnd/data/chain/bitcoin/mainnet",
		TLSPath:     "/home/walter/.lnd/tls.cert",
	})
	if err != nil {
		fmt.Printf("%v", err)
		panic(err)
	}

	s.lnd = lnd.Client
	s.invoice_client = lnd.Invoices
	s.router = lnd.Router
	countdown := newCountDownTimer(10 * time.Second)
	s.countdown = countdown
	return &s
}

func (n *State) readDisplayState() *displayState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	time_left := n.countdown.timeLeft()

	return &displayState{n.tickets, n.winners, time_left, n.getPayoutSize()}
}

func (n *State) CountdownTimerChannel() <-chan time.Time {
	return n.countdown.ticker.C
}

func (n *State) addTicket(ticket *Ticket) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tickets = append(n.tickets, ticket)
	n.pot += ticket.AmountSats
}

func (n *State) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	totalNumberOfTickets := 0
	for _, ticket := range n.tickets {
		totalNumberOfTickets += int(ticket.AmountSats)
	}

	if totalNumberOfTickets > 0 {
		n.selectWinner(totalNumberOfTickets)
	}

	n.tickets = nil
	n.pot = 0
	n.countdown.lastTick = time.Now()
}

func (n *State) selectWinner(totalNumberOfTickets int) {
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
	if selected_node_id != myNodeID {
		n.payWinner(selected_node_id)
	}
	n.winners = append(n.winners, &Winner{selected_node_id, n.getPayoutSize()})
}

func (n *State) payWinner(nodeID string) {
	vertex, err := route.NewVertexFromStr(nodeID)
	if err != nil {
		fmt.Printf("ERROR %v\n", err)
		return
	}
	request := lndclient.SendPaymentRequest{}
	request.KeySend = true
	request.Amount = btcutil.Amount(n.getPayoutSize())
	request.MaxFee = btcutil.Amount(float64(n.getPayoutSize()) * 0.001)
	request.Timeout = time.Minute
	request.Target = vertex
	paymentChan, errChan, err := n.router.SendPayment(context.Background(), request)
	if err != nil {
		fmt.Printf("ERROR %v", err)
	} else {
		go func() {
			for {
				select {
				case err = <-errChan:
					fmt.Printf("ERROR %v\n", err)
					return
				case status := <-paymentChan:
					// TODO: Update winner status once status field is added
					fmt.Printf("Payment status changed: %v\n", status)
					if status.State == lnrpc.Payment_SUCCEEDED || status.State == lnrpc.Payment_FAILED {
						return
					}
				}
			}
		}()
	}
}

func (n *State) handlePollInvoiceWs(ws *websocket.Conn, hash lntypes.Hash) {
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
