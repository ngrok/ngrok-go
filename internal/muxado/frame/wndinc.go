package frame

import (
	"fmt"
	"io"
)

const (
	wndIncFrameLength = 4
)

// Increase a stream's flow control window size
type WndInc struct {
	common
}

func (f *WndInc) WindowIncrement() uint32 {
	return order.Uint32(f.body()) & wndIncMask
}

func (f *WndInc) readFrom(rd io.Reader) error {
	if f.length != wndIncFrameLength {
		return frameSizeError(f.length, "WNDINC")
	}
	if _, err := io.ReadFull(rd, f.body()[:wndIncFrameLength]); err != nil {
		return err
	}
	if f.StreamId() == 0 {
		return protoError("WNDINC stream id must not be zero, got: %d", f.StreamId())
	}
	if f.WindowIncrement() == 0 {
		return protoStreamError("WNDINC increment must not be zero, got: %d", f.WindowIncrement())
	}
	return nil
}

func (f *WndInc) writeTo(wr io.Writer) error {
	return f.common.writeTo(wr, wndIncFrameLength)
}

func (f *WndInc) Pack(streamId StreamId, inc uint32) (err error) {
	if inc > wndIncMask || inc == 0 {
		return fmt.Errorf("invalid window increment: %d", inc)
	}
	if err = f.common.pack(TypeWndInc, wndIncFrameLength, streamId, 0); err != nil {
		return
	}
	order.PutUint32(f.body(), inc)
	return
}
