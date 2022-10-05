package throughput

import (
	"encoding/json"
	"fmt"
	"time"
)

const interval time.Duration = time.Second

type Throughput struct {
	unit string

	workQueue chan int
	resetChan chan bool

	count int

	min      int
	max      int
	avg      int
	total    int
	duration time.Duration
}

func New(unit string, done <-chan struct{}) *Throughput {
	t := &Throughput{
		unit:      unit,
		workQueue: make(chan int, 100),
		resetChan: make(chan bool),
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if t.count < t.min {
					t.min = t.count
				}

				if t.count > t.max {
					t.max = t.count
				}

				t.avg = ((t.avg*t.total)+t.count)/t.total + 1

				t.total += t.count
				t.duration += interval

				// empty out our current window
				t.count = 0
			case e := <-t.workQueue:
				t.count += e
			case <-t.resetChan:
				t.count = 0
				t.min = 100000
				t.max = 0
				t.avg = 0
				t.total = 0
				t.duration = 0
			}
		}
	}()

	return t
}

func (t *Throughput) Count(n int) {
	t.workQueue <- n
}

func (t *Throughput) Reset() {
	t.resetChan <- true
}

func (t *Throughput) String() json.RawMessage {
	m := map[string]string{
		"Min":      fmt.Sprintf("%d %s/s", t.min, t.unit),
		"Max":      fmt.Sprintf("%d %s/s", t.max, t.unit),
		"Avg":      fmt.Sprintf("%d %s/s", t.avg, t.unit),
		"Total":    fmt.Sprintf("%d %s", t.total, t.unit),
		"Duration": fmt.Sprintf("%d seconds", int(t.duration.Seconds())),
	}

	r, _ := json.Marshal(m)
	return r
}
