package muxado

import (
	"io"
	"sync"

	"github.com/ngrok/libngrok-go/internal/muxado/frame"
)

var zeroConfig Config

func init() {
	zeroConfig.initDefaults()
}

type Config struct {
	// Maximum size of unread data to receive and buffer (per-stream). Default 256KB.
	MaxWindowSize uint32
	// Maximum number of inbound streams to queue for Accept(). Default 128.
	AcceptBacklog uint32
	// Function creating the Session's framer. Deafult frame.NewFramer()
	NewFramer func(io.Reader, io.Writer) frame.Framer

	// allow safe concurrent initialization
	initOnce sync.Once

	// Function to create new streams
	newStream streamFactory

	// Size of writeFrames channel
	writeFrameQueueDepth int
}

func (c *Config) initDefaults() {
	c.initOnce.Do(func() {
		if c.MaxWindowSize == 0 {
			c.MaxWindowSize = 0x40000 // 256KB
		}
		if c.AcceptBacklog == 0 {
			c.AcceptBacklog = 128
		}
		if c.NewFramer == nil {
			c.NewFramer = frame.NewFramer
		}
		if c.newStream == nil {
			c.newStream = newStream
		}
		if c.writeFrameQueueDepth == 0 {
			c.writeFrameQueueDepth = 64
		}
	})
}
