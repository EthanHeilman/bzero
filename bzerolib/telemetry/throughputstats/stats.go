package throughputstats

import (
	"encoding/json"

	"bastionzero.com/bctl/v1/bzerolib/telemetry/throughput"
)

type Digest struct {
	Inbound  json.RawMessage `json:"inbound"`
	Outbound json.RawMessage `json:"outbound"`
}

type ThroughputStats struct {
	inbound  throughput.Throughput
	outbound throughput.Throughput
}

func New(unit string, done <-chan struct{}) *ThroughputStats {
	return &ThroughputStats{
		inbound:  *throughput.New(unit, done),
		outbound: *throughput.New(unit, done),
	}
}

func (c *ThroughputStats) Reset() {
	c.inbound.Reset()
	c.outbound.Reset()
}

func (c *ThroughputStats) CountInbound(n int) {
	c.inbound.Count(n)
}

func (c *ThroughputStats) CountOutbound(n int) {
	c.outbound.Count(n)
}

func (c *ThroughputStats) Digest() Digest {
	return Digest{
		Inbound:  c.inbound.String(),
		Outbound: c.outbound.String(),
	}
}
