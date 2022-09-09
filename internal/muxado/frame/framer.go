package frame

import (
	"fmt"
	"io"
	"sync"
	"text/tabwriter"
)

type Frame interface {
	StreamId() StreamId
	Type() Type
	Flags() Flags
	Length() uint32
	readFrom(io.Reader) error
	writeTo(io.Writer) error
}

// A Framer serializes/deserializer frames to/from an io.ReadWriter
type Framer interface {
	// WriteFrame writes the given frame to the underlying transport
	WriteFrame(Frame) error

	// ReadFrame reads the next frame from the underlying transport
	ReadFrame() (Frame, error)
}

type framer struct {
	io.Reader
	io.Writer
	common

	// frames
	Rst
	Data
	WndInc
	GoAway
	Unknown
}

func (fr *framer) WriteFrame(f Frame) error {
	return f.writeTo(fr.Writer)
}

func (fr *framer) ReadFrame() (f Frame, err error) {
	if err := fr.common.readFrom(fr.Reader); err != nil {
		return nil, err
	}
	switch fr.common.ftype {
	case TypeRst:
		f = &fr.Rst
		fr.Rst.common = fr.common
	case TypeData:
		f = &fr.Data
		fr.Data.common = fr.common
	case TypeWndInc:
		f = &fr.WndInc
		fr.WndInc.common = fr.common
	case TypeGoAway:
		f = &fr.GoAway
		fr.GoAway.common = fr.common
	default:
		f = &fr.Unknown
		fr.Unknown.common = fr.common
	}
	return f, f.readFrom(fr)
}

func NewFramer(r io.Reader, w io.Writer) Framer {
	fr := &framer{
		Reader: r,
		Writer: w,
	}
	return fr
}

type debugFramer struct {
	sync.Mutex                   // protects debugWr
	debugWr    *tabwriter.Writer // must be protected by mutex
	once       sync.Once
	name       string
	Framer
}

func (fr *debugFramer) flushWriter() {
	fr.Lock()
	defer fr.Unlock()
	fr.debugWr.Flush()
}

func (fr *debugFramer) WriteFrame(f Frame) error {
	defer fr.flushWriter()
	fr.printHeader()

	// actually write the frame to the real framer
	err := fr.Framer.WriteFrame(f)

	fr.Lock()
	defer fr.Unlock()
	// each frame knows how to write iteself to the framer
	fmt.Fprintf(fr.debugWr, "%s\t%s\t%s\t0x%x\t%d\t0x%x\t%v\n", fr.name, "WRITE", f.Type(), f.StreamId(), f.Length(), f.Flags(), err)
	return err
}

func (fr *debugFramer) ReadFrame() (Frame, error) {
	defer fr.flushWriter()
	fr.printHeader()
	f, err := fr.Framer.ReadFrame()
	fr.Lock()
	defer fr.Unlock()
	if err == nil {
		fmt.Fprintf(fr.debugWr, "%s\t%s\t%s\t0x%x\t%d\t0x%x\t%v\n", fr.name, "READ", f.Type(), f.StreamId(), f.Length(), f.Flags(), nil)
	} else {
		fmt.Fprintf(fr.debugWr, "%s\t%s\t\t\t\t\t%v\n", fr.name, "READ", err)
	}
	return f, err
}

func (fr *debugFramer) printHeader() {
	fr.once.Do(func() {
		fr.Lock()
		defer fr.Unlock()
		fmt.Fprintf(fr.debugWr, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", "NAME", "OP", "TYPE", "STREAMID", "LENGTH", "FLAGS", "ERROR")
		fmt.Fprintf(fr.debugWr, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", "----", "--", "----", "--------", "------", "-----", "-----")
	})
}

func NewDebugFramer(wr io.Writer, fr Framer) Framer {
	return NewNamedDebugFramer("", wr, fr)
}

func NewNamedDebugFramer(name string, wr io.Writer, fr Framer) Framer {
	return &debugFramer{
		Framer:  fr,
		debugWr: tabwriter.NewWriter(wr, 12, 2, 2, ' ', 0),
		name:    name,
	}
}
