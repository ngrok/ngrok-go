package muxado

import (
	"encoding/binary"
	"io"
	"math/rand"
	"net"
	"sync/atomic"
	"time"
)

const (
	defaultHeartbeatInterval             = 10 * time.Second
	defaultHeartbeatTolerance            = 15 * time.Second
	defaultStreamType         StreamType = 0xFFFFFFFF
)

type HeartbeatSession interface {
        TypedStreamSession
        Beat() time.Duration
        Start()
        SetInterval(d time.Duration)
        SetTolerance(d time.Duration)
}

type HeartbeatConfig struct {
	Interval  time.Duration
	Tolerance time.Duration
	Type      StreamType
}

func NewHeartbeatConfig() *HeartbeatConfig {
	return &HeartbeatConfig{
		Interval:  defaultHeartbeatInterval,
		Tolerance: defaultHeartbeatTolerance,
		Type:      defaultStreamType,
	}
}

type Heartbeat struct {
	// atomically accessed, must be first in structure for ARM/x86 alignment
	interval  int64
	tolerance int64

	TypedStreamSession
	config HeartbeatConfig
	closed chan int
	cb     func(time.Duration)

	onDemand chan chan time.Duration
}

func NewHeartbeat(sess TypedStreamSession, cb func(time.Duration), config *HeartbeatConfig) *Heartbeat {
	if config == nil {
		config = NewHeartbeatConfig()
	}
	return &Heartbeat{
		TypedStreamSession: sess,
		config:             *config,
		closed:             make(chan int, 1),
		cb:                 cb,
		interval:           int64(config.Interval),
		tolerance:          int64(config.Tolerance),

		onDemand: make(chan chan time.Duration),
	}
}

func (h *Heartbeat) Accept() (net.Conn, error) {
	return h.AcceptTypedStream()
}

func (h *Heartbeat) Beat() time.Duration {
	timeout := time.After(time.Duration(h.tolerance))
	respChan := make(chan time.Duration, 1)
	select {
	case <-timeout:
		return 0
	case h.onDemand <- respChan:
	}
	select {
	case <-timeout:
		return 0
	case latency := <-respChan:
		return latency
	}
}

func (h *Heartbeat) AcceptStream() (Stream, error) {
	return h.TypedStreamSession.AcceptTypedStream()
}

func (h *Heartbeat) SetInterval(d time.Duration) {
	atomic.StoreInt64(&h.interval, int64(d))
}

func (h *Heartbeat) SetTolerance(d time.Duration) {
	atomic.StoreInt64(&h.tolerance, int64(d))
}

func (h *Heartbeat) Close() error {
	select {
	case h.closed <- 1:
	default:
	}
	return h.TypedStreamSession.Close()
}

func (h *Heartbeat) AcceptTypedStream() (TypedStream, error) {
	for {
		str, err := h.TypedStreamSession.AcceptTypedStream()
		if err != nil {
			return nil, err
		}
		if str.StreamType() != h.config.Type {
			return str, nil
		}
		go h.responder(str)
	}
}

func (h *Heartbeat) Start() {
	mark := make(chan time.Duration)
	go h.requester(mark)
	go h.check(mark)
}

func (h *Heartbeat) check(mark chan time.Duration) {
	interval, tolerance := h.getDurations()
	t := time.NewTimer(interval + tolerance)
	for {
		select {
		case <-t.C:
			// timed out waiting for a response!
			h.cb(0)

		case dur := <-mark:
			h.cb(dur)
			interval, tolerance := h.getDurations()

			// this is the only way to safely reset a go timer
			if !t.Stop() {
				<-t.C
			}
			t.Reset(interval + tolerance)

		case <-h.closed:
			return
		}
	}
}

func (h *Heartbeat) getDurations() (time.Duration, time.Duration) {
	return time.Duration(atomic.LoadInt64(&h.interval)), time.Duration(atomic.LoadInt64(&h.tolerance))
}

func (h *Heartbeat) requester(mark chan time.Duration) {
	// make random number generator
	r := rand.New(rand.NewSource(time.Now().Unix()))

	// open a new stream for the heartbeat
	stream, err := h.OpenTypedStream(h.config.Type)
	if err != nil {
		return
	}
	defer stream.Close()

	interval, _ := h.getDurations()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// send heartbeats and then check that we got them back
	for {
		var respChan chan time.Duration
		select {
		case respChan = <-h.onDemand:
		case <-ticker.C:
		}

		start := time.Now()
		// assign a new random value to echo
		id := uint32(r.Int31())
		if err := binary.Write(stream, binary.BigEndian, id); err != nil {
			return
		}
		var respId uint32
		if err := binary.Read(stream, binary.BigEndian, &respId); err != nil {
			return
		}
		if id != respId {
			return
		}

		latency := time.Since(start)

		// record the time
		if respChan != nil {
			respChan <- latency
		} else {
			mark <- time.Since(start)
		}
	}
}

func (h *Heartbeat) responder(s Stream) {
	// read the next heartbeat id and respond
	buf := make([]byte, 4)
	for {
		_, err := io.ReadFull(s, buf)
		if err != nil {
			return
		}
		_, err = s.Write(buf)
		if err != nil {
			return
		}
	}
}
