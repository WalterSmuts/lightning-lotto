package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"strconv"
	"time"
)

import qrcode "github.com/skip2/go-qrcode"

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type ticket struct {
	nodeID     string
	amountSats uint64
}

type countdownTimer struct {
	ticker   time.Ticker
	lastTick time.Time
}

func (t *ticket) String() string {
	return fmt.Sprintf("%s:%d\n", t.nodeID, t.amountSats)
}

type state struct {
	tickets   []*ticket
	countdown countdownTimer
}

func (n *state) addTicketRequest(w http.ResponseWriter, req *http.Request) {
	nodeID := req.URL.Query().Get("node_id")
	amountSatsString := req.URL.Query().Get("amount")
	amountSats, err := strconv.ParseInt(amountSatsString, 10, 64)
	if err != nil {
		fmt.Fprintf(w, "ERROR %v", err)
		return
	}

	n.tickets = append(n.tickets, &ticket{nodeID, uint64(amountSats)})

	fmt.Fprintf(w, n.printState())
}

func (n *state) handlePollInvoiceRequest(w http.ResponseWriter, req *http.Request) {
	ws, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		fmt.Fprintf(w, "ERROR %v", err)
		return
	}
	time.Sleep(2 * time.Second)
	ws.WriteMessage(0, []byte("Paid"))
}

func handleInvoiceQR(w http.ResponseWriter, req *http.Request) {
	png, err := qrcode.Encode("https://example.org", qrcode.Medium, 256)
	if err != nil {
		fmt.Fprintf(w, "ERROR %v", err)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func (n *state) printTickets(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, n.printState())
}

func (n *state) printState() string {
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

	http.HandleFunc("/add_ticket_request", s.addTicketRequest)
	http.HandleFunc("/", s.printTickets)
	http.HandleFunc("/invoice_qr", handleInvoiceQR)
	http.HandleFunc("/ws/poll_invoice", s.handlePollInvoiceRequest)
	http.ListenAndServe(":8090", nil)
}
