package state

import (
	"context"
	"encoding/json"
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
	"github.com/waltersmuts/lightning-lotto/config"
)

type Ticket struct {
	NodeID     string
	AmountSats uint64
}

type Winner struct {
	NodeID     string
	AmountSats uint64
}

type CountdownTimer struct {
	ticker   time.Ticker
	lastTick time.Time
	duration time.Duration
}

func (t CountdownTimer) TimeLeft() time.Duration {
	timeLeft := (t.duration - time.Now().Sub(t.lastTick))
	return timeLeft
}

func newCountDownTimer(duration time.Duration) CountdownTimer {
	return CountdownTimer{*time.NewTicker(duration), time.Now(), duration}
}

func (t *Ticket) String() string {
	return fmt.Sprintf("%s:%d\n", t.NodeID, t.AmountSats)
}

type State struct {
	tickets         []*Ticket
	ticketObservers map[chan Ticket]struct{}
	winners         []*Winner
	Countdown       CountdownTimer
	mu              sync.RWMutex
	pot             uint64
	Lnd             lndclient.LightningClient
	Invoice_client  lndclient.InvoicesClient
	Router          lndclient.RouterClient
}

func (n *State) getPayoutSize() uint64 {
	return uint64(float64(n.pot) * 0.99)
}

type DisplayState struct {
	Tickets  []*Ticket
	Winners  []*Winner
	TimeLeft time.Duration
	Pot      uint64
}

func NewState() *State {
	var s State
	lnd, err := lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:  "localhost",
		Network:     config.Config.Network,
		MacaroonDir: config.Config.MacaroonDir,
		TLSPath:     config.Config.TLSPath,
	})
	if err != nil {
		fmt.Printf("%v", err)
		panic(err)
	}

	s.Lnd = lnd.Client
	s.Invoice_client = lnd.Invoices
	s.Router = lnd.Router
	countdown := newCountDownTimer(2 * time.Minute)
	s.Countdown = countdown
	s.setStartingValues()
	s.ticketObservers = make(map[chan Ticket]struct{})
	return &s
}

func (n *State) ReadDisplayState() *DisplayState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	timeLeft := n.Countdown.TimeLeft()

	return &DisplayState{n.tickets, n.winners, timeLeft, n.getPayoutSize()}
}

func (n *State) CountdownTimerChannel() <-chan time.Time {
	return n.Countdown.ticker.C
}

func (n *State) AddTicket(ticket *Ticket) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.addTicketUnsafe(ticket)
}

func (n *State) addTicketUnsafe(ticket *Ticket) {
	n.tickets = append(n.tickets, ticket)
	n.pot += ticket.AmountSats

	// This will deadlock if we don't execute in different go-routine
	go n.notifyAllTicketObservers(*ticket)
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
	n.setStartingValues()
}

func (n *State) setStartingValues() {
	n.tickets = nil
	n.pot = 0
	n.Countdown.lastTick = time.Now()
	defaultTicket := Ticket{config.Config.MyNodeID, 10}
	n.addTicketUnsafe(&defaultTicket)
}

func (n *State) selectWinner(totalNumberOfTickets int) {
	selected := rand.Intn(totalNumberOfTickets)
	ticketSum := 0

	previousTicketSum := 0
	selectedNodeID := "Unknown"
	for _, ticket := range n.tickets {
		ticketSum += int(ticket.AmountSats)
		if previousTicketSum <= selected && ticketSum > selected {
			selectedNodeID = ticket.NodeID
			break
		}
		previousTicketSum = ticketSum
	}
	if selectedNodeID != config.Config.MyNodeID {
		n.payWinner(selectedNodeID)
	}
	n.winners = append(n.winners, &Winner{selectedNodeID, n.getPayoutSize()})
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
	paymentChan, errChan, err := n.Router.SendPayment(context.Background(), request)
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

func (n *State) HandlePollInvoiceWs(ws *websocket.Conn, hash lntypes.Hash) {
	updateChan, errChan, err := n.Invoice_client.SubscribeSingleInvoice(context.Background(), hash)
	if err != nil {
		fmt.Printf("ERROR %v", err)
		return
	}
	for {
		select {
		case update := <-updateChan:
			fmt.Printf("Payment %v has changed to state %v\n", hash, update)
			if update.State == channeldb.ContractSettled {
				err = ws.WriteMessage(websocket.TextMessage, []byte("Paid"))
				if err != nil {
					fmt.Printf("ERR %v\n", err)
					return
				}

				return
			}
		case err := <-errChan:
			fmt.Printf("ERROR %v\n", err)
		}
	}
}
func (n *State) registerTicketStream() chan Ticket {
	n.mu.Lock()
	defer n.mu.Unlock()

	type void struct{}
	var member void

	channel := make(chan Ticket, len(n.tickets))

	for _, ticket := range n.tickets {
		channel <- *ticket
	}
	n.ticketObservers[channel] = member
	fmt.Printf("Registering ticket observer\n")

	return channel
}

func (n *State) deregisterTicketStream(ticket_chan chan Ticket) {
	fmt.Printf("Deregistering ticket observer\n")
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.ticketObservers, ticket_chan)
}

func (n *State) notifyAllTicketObservers(ticket Ticket) {
	fmt.Println("Starting notification of ticket observers")
	n.mu.RLock()
	defer n.mu.RUnlock()

	for observer := range n.ticketObservers {
		observer <- ticket
	}
	fmt.Println("Done notifying ticket observers")

}

func (n *State) HandleStreamTicketsWs(ws *websocket.Conn) {
	updateChan := n.registerTicketStream()
	defer n.deregisterTicketStream(updateChan)
	ws.SetCloseHandler(func(code int, text string) error {
		n.deregisterTicketStream(updateChan)
		return nil
	})

	for {
		ticket := <-updateChan
		json, err := json.Marshal(ticket)
		if err != nil {
			fmt.Printf("ERR %v\n", err)
			return
		}

		err = ws.WriteMessage(websocket.TextMessage, json)
		if err != nil {
			fmt.Printf("ERR %v\n", err)
			return
		}
	}
}
