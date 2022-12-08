package monitor

import (
	"bastionzero.com/bctl/v1/bzerolib/telemetry/throughput"
)

type StatsMonitor struct {
	// General stats we keep track of
	OpenedDataChannels int `json:"OpenedDataChannels"`
	ClosedDataChannels int `json:"ClosedDataChannels"`

	InboundAgentMessage  *throughput.Throughput `json:"inboundAgentMessage"`
	OutboundAgentMessage *throughput.Throughput `json:"outboundAgentMessage"`

	InboundSignalR  *throughput.Throughput `json:"inboundSignalR"`
	OutboundSignalR *throughput.Throughput `json:"outboundSignalR"`

	InboundBytes  *throughput.Throughput `json:"inboundBytes"`
	OutboundBytes *throughput.Throughput `json:"outboundBytes"`
}

func New(done <-chan struct{}) *StatsMonitor {
	return &StatsMonitor{
		InboundAgentMessage:  throughput.New("AgentMessages", done),
		OutboundAgentMessage: throughput.New("AgentMessages", done),
		InboundSignalR:       throughput.New("SignalR Messages", done),
		OutboundSignalR:      throughput.New("SignalR Messages", done),
		InboundBytes:         throughput.New("bytes", done),
		OutboundBytes:        throughput.New("bytes", done),
	}
}

// this resets the throughput windows so that we don't resend previous data in
// our heartbeat messages
func (m *StatsMonitor) ResetThroughputWindow() {
	m.InboundAgentMessage.Reset()
	m.OutboundAgentMessage.Reset()
	m.InboundSignalR.Reset()
	m.OutboundSignalR.Reset()
	m.InboundBytes.Reset()
	m.OutboundBytes.Reset()
}

// Observe inbound (relative to agent) AgentMessages for throughput calculations
func (m *StatsMonitor) ObserveInboundAgentMessage() {
	m.InboundAgentMessage.Observe(1)
}

// Observe outbound (relative to agent) AgentMessages for throughput calculations
func (m *StatsMonitor) ObserveOutboundAgentMessage() {
	m.OutboundAgentMessage.Observe(1)
}

// Observe inbound (relative to agent) SignalR messages for throughput calculations
func (m *StatsMonitor) ObserveInboundSignalR() {
	m.InboundSignalR.Observe(1)
}

// Observe outbound (relative to agent) SignalR messages for throughput calculations
func (m *StatsMonitor) ObserveOutboundSignalR() {
	m.OutboundSignalR.Observe(1)
}

// Observe inbound (relative to agent) raw bytes for throughput calculations
func (m *StatsMonitor) ObserveInboundBytes(n int) {
	m.InboundBytes.Observe(n)
}

// Observe outbound (relative to agent) raw bytes for throughput calculations
func (m *StatsMonitor) ObserveOutboundBytes(n int) {
	m.OutboundBytes.Observe(n)
}

func (m *StatsMonitor) ObserveOpenDatachannel() {
	m.OpenedDataChannels++
}

func (m *StatsMonitor) ObserveCloseDatachannel() {
	m.ClosedDataChannels++
}
