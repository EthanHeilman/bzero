package throughput

import (
	"time"
)

const interval time.Duration = time.Second

type Throughput struct {
	unit      string
	count     int
	workQueue chan int
	resetChan chan bool

	Total int       `json:"total"`
	Start time.Time `json:"start"`
	Stop  time.Time `json:"stop"`
	Data  []int     `json:"data"`
}

func New(unit string, done <-chan struct{}) *Throughput {
	t := Throughput{
		unit:      unit,
		workQueue: make(chan int, 15),
		resetChan: make(chan bool),
		Start:     time.Now(),
		Stop:      time.Now(),
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				t.Stop = time.Now().UTC()
				t.Total += t.count
				t.Data = append(t.Data, t.count)

				// empty out our current window
				t.count = 0
			case e := <-t.workQueue:
				t.count += e
			case <-t.resetChan:
				t.count = 0
				t.Total = 0
				t.Start = time.Now().UTC()
				t.Stop = time.Now().UTC()
				t.Data = []int{}
			}
		}
	}()

	return &t
}

func (t *Throughput) Observe(n int) {
	t.workQueue <- n
}

func (t *Throughput) Reset() {
	t.resetChan <- true
}
