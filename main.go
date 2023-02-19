package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type ticket struct {
	nodeID     string
	amountSats uint64
}

func (t *ticket) String() string {
	return fmt.Sprintf("%s:%d\n", t.nodeID, t.amountSats)
}

type ticketList struct {
	tickets []*ticket
}

func (n *ticketList) addTicketRequest(w http.ResponseWriter, req *http.Request) {
	nodeID := req.URL.Query().Get("node_id")
	amountSatsString := req.URL.Query().Get("amount")
	amountSats, err := strconv.ParseInt(amountSatsString, 10, 64)
	if err != nil {
		fmt.Fprintf(w, "ERROR %v", err)
		return
	}

	n.tickets = append(n.tickets, &ticket{nodeID, uint64(amountSats)})

	var result string
	for _, t := range n.tickets {
		result += t.String()
	}
	fmt.Fprintf(w, result)
}

func (n *ticketList) printTickets(w http.ResponseWriter, req *http.Request) {
	var result string
	for _, t := range n.tickets {
		result += t.String()
	}
	fmt.Fprintf(w, result)
}

func main() {
	var list ticketList
	ticker := time.NewTicker(10 * time.Second)
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				fmt.Println("Cleared at", t)
				list.tickets = nil
			}
		}
	}()

	http.HandleFunc("/add_ticket_request", list.addTicketRequest)
	http.HandleFunc("/", list.printTickets)
	http.ListenAndServe(":8090", nil)
}
