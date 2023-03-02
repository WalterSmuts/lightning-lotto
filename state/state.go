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

func (state *State) getPayoutSize() uint64 {
	return uint64(float64(state.pot) * 0.99)
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

	go func() {
		for {
			t := <-countdown.ticker.C
			fmt.Println("Cleared at", t)
			s.Reset()
		}
	}()

	s.Countdown = countdown
	s.setStartingValues()
	s.ticketObservers = make(map[chan Ticket]struct{})
	return &s
}

func (state *State) ReadDisplayState() *DisplayState {
	state.mu.RLock()
	defer state.mu.RUnlock()
	timeLeft := state.Countdown.TimeLeft()

	return &DisplayState{state.tickets, state.winners, timeLeft, state.getPayoutSize()}
}

func (state *State) AddTicket(ticket *Ticket) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.addTicketUnsafe(ticket)
}

func (state *State) addTicketUnsafe(ticket *Ticket) {
	state.tickets = append(state.tickets, ticket)
	state.pot += ticket.AmountSats

	// This will deadlock if we don't execute in different go-routine
	go state.notifyAllTicketObservers(*ticket)
}

func (state *State) Reset() {
	state.mu.Lock()
	defer state.mu.Unlock()
	totalNumberOfTickets := 0
	for _, ticket := range state.tickets {
		totalNumberOfTickets += int(ticket.AmountSats)
	}

	if totalNumberOfTickets > 0 {
		state.selectWinner(totalNumberOfTickets)
	}
	state.setStartingValues()
}

func (state *State) setStartingValues() {
	state.tickets = nil
	state.pot = 0
	state.Countdown.lastTick = time.Now()
	defaultTicket := Ticket{config.Config.MyNodeID, 10}
	state.addTicketUnsafe(&defaultTicket)
}

func (state *State) selectWinner(totalNumberOfTickets int) {
	selected := rand.Intn(totalNumberOfTickets)
	ticketSum := 0

	previousTicketSum := 0
	selectedNodeID := "Unknown"
	for _, ticket := range state.tickets {
		ticketSum += int(ticket.AmountSats)
		if previousTicketSum <= selected && ticketSum > selected {
			selectedNodeID = ticket.NodeID
			break
		}
		previousTicketSum = ticketSum
	}
	if selectedNodeID != config.Config.MyNodeID {
		state.payWinner(selectedNodeID)
	}
	state.winners = append(state.winners, &Winner{selectedNodeID, state.getPayoutSize()})
}

func (state *State) payWinner(nodeID string) {
	vertex, err := route.NewVertexFromStr(nodeID)
	if err != nil {
		fmt.Printf("ERROR %v\n", err)
		return
	}
	request := lndclient.SendPaymentRequest{}
	request.KeySend = true
	request.Amount = btcutil.Amount(state.getPayoutSize())
	request.MaxFee = btcutil.Amount(float64(state.getPayoutSize()) * 0.001)
	request.Timeout = time.Minute
	request.Target = vertex
	paymentChan, errChan, err := state.Router.SendPayment(context.Background(), request)
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

func (state *State) HandlePollInvoiceWs(ws *websocket.Conn, hash lntypes.Hash) {
	updateChan, errChan, err := state.Invoice_client.SubscribeSingleInvoice(context.Background(), hash)
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
func (state *State) registerTicketStream() chan Ticket {
	state.mu.Lock()
	defer state.mu.Unlock()

	type void struct{}
	var member void

	channel := make(chan Ticket, len(state.tickets))

	for _, ticket := range state.tickets {
		channel <- *ticket
	}
	state.ticketObservers[channel] = member
	fmt.Printf("Registering ticket observer\n")

	return channel
}

func (state *State) deregisterTicketStream(ticket_chan chan Ticket) {
	fmt.Printf("Deregistering ticket observer\n")
	state.mu.Lock()
	defer state.mu.Unlock()
	delete(state.ticketObservers, ticket_chan)
}

func (state *State) notifyAllTicketObservers(ticket Ticket) {
	fmt.Println("Starting notification of ticket observers")
	state.mu.RLock()
	defer state.mu.RUnlock()

	for observer := range state.ticketObservers {
		observer <- ticket
	}
	fmt.Println("Done notifying ticket observers")

}

func (state *State) HandleStreamTicketsWs(ws *websocket.Conn) {
	updateChan := state.registerTicketStream()
	defer state.deregisterTicketStream(updateChan)
	ws.SetCloseHandler(func(code int, text string) error {
		state.deregisterTicketStream(updateChan)
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
