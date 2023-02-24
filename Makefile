.PHONY: lightning-lotto
lightning-lotto:
	go build ./cmd/lightning-lotto

.PHONY: run
run: lightning-lotto
	./lightning-lotto
