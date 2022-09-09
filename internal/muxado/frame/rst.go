package frame

import "io"

const (
	rstFrameLength = 4
)

// Rst is a frame sent to forcibly close a stream
type Rst struct {
	common
}

func (f *Rst) ErrorCode() ErrorCode {
	return ErrorCode(order.Uint32(f.body()))
}

func (f *Rst) readFrom(rd io.Reader) (err error) {
	if f.length != rstFrameLength {
		return frameSizeError(f.length, "RST")
	}
	if _, err = io.ReadFull(rd, f.body()[:rstFrameLength]); err != nil {
		return err
	}
	if f.StreamId() == 0 {
		return protoError("RST stream id must not be zero")
	}
	return
}

func (f *Rst) writeTo(wr io.Writer) (err error) {
	return f.common.writeTo(wr, rstFrameLength)
}

func (f *Rst) Pack(streamId StreamId, errorCode ErrorCode) (err error) {
	if err = f.common.pack(TypeRst, rstFrameLength, streamId, 0); err != nil {
		return
	}
	order.PutUint32(f.body(), uint32(errorCode))
	return
}
