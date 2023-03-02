package timer

import (
	"time"
)

type CountdownTimer struct {
	ticker   time.Ticker
	lastTick time.Time
	duration time.Duration
}

func (t CountdownTimer) TimeLeft() time.Duration {
	timeLeft := (t.duration - time.Now().Sub(t.lastTick))
	return timeLeft
}

func NewCountDownTimer(duration time.Duration, f func()) CountdownTimer {
	timer := CountdownTimer{*time.NewTicker(duration), time.Now(), duration}
	go func() {
		for {
			// Block until timer expires
			_ = <-timer.Channel()

			f()
			timer.reset()
		}
	}()
	return timer
}

func (timer *CountdownTimer) Channel() <-chan time.Time {
	return timer.ticker.C
}

func (timer *CountdownTimer) reset() {
	timer.lastTick = time.Now()
}
