package webrtc

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/artpar/terminal-tunnel/internal/crypto"
)

// TestPeerPair represents a connected pair of peers for testing
type TestPeerPair struct {
	HostPeer      *Peer
	ClientPeer    *Peer
	HostDC        *webrtc.DataChannel
	ClientDC      *webrtc.DataChannel
	HostChannel   *EncryptedChannel
	ClientChannel *EncryptedChannel
	Key           [32]byte
}

// NewTestPeerPair creates a fully connected peer pair for testing
func NewTestPeerPair(password string) (*TestPeerPair, error) {
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i)
	}
	key := crypto.DeriveKey(password, salt)

	// Create host peer
	hostPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		return nil, err
	}

	// Create client peer
	clientPeer, err := NewPeer(DefaultConfig())
	if err != nil {
		hostPeer.Close()
		return nil, err
	}

	// Create data channel on host
	hostDC, err := hostPeer.CreateDataChannel("terminal")
	if err != nil {
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	// Track client data channel
	clientDCChan := make(chan *webrtc.DataChannel, 1)
	clientPeer.OnDataChannel(func(dc *webrtc.DataChannel) {
		clientDCChan <- dc
	})

	// Exchange SDP
	offer, err := hostPeer.CreateOffer()
	if err != nil {
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	if err := clientPeer.SetRemoteDescription(webrtc.SDPTypeOffer, offer); err != nil {
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	answer, err := clientPeer.CreateAnswer()
	if err != nil {
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	if err := hostPeer.SetRemoteDescription(webrtc.SDPTypeAnswer, answer); err != nil {
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	// Wait for client data channel
	var clientDC *webrtc.DataChannel
	select {
	case clientDC = <-clientDCChan:
	case <-time.After(10 * time.Second):
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	// Wait for data channels to open
	hostDCOpen := make(chan bool, 1)
	clientDCOpen := make(chan bool, 1)
	hostDC.OnOpen(func() { hostDCOpen <- true })
	clientDC.OnOpen(func() { clientDCOpen <- true })

	select {
	case <-hostDCOpen:
	case <-time.After(10 * time.Second):
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	select {
	case <-clientDCOpen:
	case <-time.After(10 * time.Second):
		hostPeer.Close()
		clientPeer.Close()
		return nil, err
	}

	// Create encrypted channels
	hostChannel := NewEncryptedChannel(hostDC, &key)
	clientChannel := NewEncryptedChannel(clientDC, &key)

	return &TestPeerPair{
		HostPeer:      hostPeer,
		ClientPeer:    clientPeer,
		HostDC:        hostDC,
		ClientDC:      clientDC,
		HostChannel:   hostChannel,
		ClientChannel: clientChannel,
		Key:           key,
	}, nil
}

// Close closes both peers
func (p *TestPeerPair) Close() {
	if p.HostChannel != nil {
		p.HostChannel.Close()
	}
	if p.ClientChannel != nil {
		p.ClientChannel.Close()
	}
	if p.HostPeer != nil {
		p.HostPeer.Close()
	}
	if p.ClientPeer != nil {
		p.ClientPeer.Close()
	}
}

// StateObserver tracks connection state transitions
type StateObserver struct {
	states      []webrtc.PeerConnectionState
	mu          sync.Mutex
	stateChange chan webrtc.PeerConnectionState
}

// NewStateObserver creates a new state observer
func NewStateObserver() *StateObserver {
	return &StateObserver{
		states:      make([]webrtc.PeerConnectionState, 0),
		stateChange: make(chan webrtc.PeerConnectionState, 100),
	}
}

// OnStateChange records state changes
func (o *StateObserver) OnStateChange(state webrtc.PeerConnectionState) {
	o.mu.Lock()
	o.states = append(o.states, state)
	o.mu.Unlock()

	select {
	case o.stateChange <- state:
	default:
	}
}

// WaitForState waits for a specific state with timeout
func (o *StateObserver) WaitForState(target webrtc.PeerConnectionState, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case state := <-o.stateChange:
			if state == target {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// GetHistory returns state transition history
func (o *StateObserver) GetHistory() []webrtc.PeerConnectionState {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]webrtc.PeerConnectionState, len(o.states))
	copy(result, o.states)
	return result
}

// HasState checks if a state was ever reached
func (o *StateObserver) HasState(target webrtc.PeerConnectionState) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, s := range o.states {
		if s == target {
			return true
		}
	}
	return false
}

// NetworkSimulator simulates network conditions for testing
type NetworkSimulator struct {
	latencyMs    int
	jitterMs     int
	packetLoss   float64
	mu           sync.Mutex
	enabled      atomic.Bool
	rng          *rand.Rand
}

// NewNetworkSimulator creates a network condition simulator
func NewNetworkSimulator() *NetworkSimulator {
	return &NetworkSimulator{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetLatency sets base latency in milliseconds
func (n *NetworkSimulator) SetLatency(ms int) {
	n.mu.Lock()
	n.latencyMs = ms
	n.mu.Unlock()
}

// SetJitter sets latency jitter in milliseconds
func (n *NetworkSimulator) SetJitter(ms int) {
	n.mu.Lock()
	n.jitterMs = ms
	n.mu.Unlock()
}

// SetPacketLoss sets packet loss percentage (0.0 to 1.0)
func (n *NetworkSimulator) SetPacketLoss(percent float64) {
	n.mu.Lock()
	n.packetLoss = percent
	n.mu.Unlock()
}

// Enable enables network simulation
func (n *NetworkSimulator) Enable() {
	n.enabled.Store(true)
}

// Disable disables network simulation
func (n *NetworkSimulator) Disable() {
	n.enabled.Store(false)
}

// Reset resets all network conditions
func (n *NetworkSimulator) Reset() {
	n.mu.Lock()
	n.latencyMs = 0
	n.jitterMs = 0
	n.packetLoss = 0
	n.mu.Unlock()
	n.enabled.Store(false)
}

// ShouldDrop returns true if packet should be dropped
func (n *NetworkSimulator) ShouldDrop() bool {
	if !n.enabled.Load() {
		return false
	}
	n.mu.Lock()
	loss := n.packetLoss
	n.mu.Unlock()
	return n.rng.Float64() < loss
}

// GetDelay returns the delay to apply (latency + jitter)
func (n *NetworkSimulator) GetDelay() time.Duration {
	if !n.enabled.Load() {
		return 0
	}
	n.mu.Lock()
	latency := n.latencyMs
	jitter := n.jitterMs
	n.mu.Unlock()

	delay := latency
	if jitter > 0 {
		delay += n.rng.Intn(jitter*2) - jitter
		if delay < 0 {
			delay = 0
		}
	}
	return time.Duration(delay) * time.Millisecond
}

// MessageCounter counts messages with thread-safety
type MessageCounter struct {
	count    atomic.Int32
	messages sync.Map
}

// NewMessageCounter creates a new message counter
func NewMessageCounter() *MessageCounter {
	return &MessageCounter{}
}

// Add increments the count
func (c *MessageCounter) Add() {
	c.count.Add(1)
}

// AddWithSeq adds with sequence number tracking
func (c *MessageCounter) AddWithSeq(seq int) {
	c.count.Add(1)
	c.messages.Store(seq, true)
}

// Count returns current count
func (c *MessageCounter) Count() int32 {
	return c.count.Load()
}

// HasSeq checks if a sequence was received
func (c *MessageCounter) HasSeq(seq int) bool {
	_, ok := c.messages.Load(seq)
	return ok
}

// MissingSeqs returns missing sequence numbers in range [start, end)
func (c *MessageCounter) MissingSeqs(start, end int) []int {
	var missing []int
	for i := start; i < end; i++ {
		if !c.HasSeq(i) {
			missing = append(missing, i)
		}
	}
	return missing
}

// WaitForCount waits for count to reach target
func (c *MessageCounter) WaitForCount(target int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for c.count.Load() < target && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return c.count.Load() >= target
}
