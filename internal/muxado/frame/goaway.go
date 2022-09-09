package frame

import "io"

const goAwayFrameLength = 8

type GoAway struct {
	common
	debugToWrite []byte
	debugToRead  io.LimitedReader
}

func (f *GoAway) LastStreamId() StreamId {
	return StreamId(order.Uint32(f.body()))
}

func (f *GoAway) ErrorCode() ErrorCode {
	return ErrorCode(order.Uint32(f.body()[4:]))
}

func (f *GoAway) Debug() io.Reader {
	return &f.debugToRead
}

func (f *GoAway) readFrom(rd io.Reader) error {
	if f.length < goAwayFrameLength {
		return frameSizeError(f.length, "GOAWAY")
	}
	if _, err := io.ReadFull(rd, f.body()[:goAwayFrameLength]); err != nil {
		return err
	}
	if f.StreamId() != 0 {
		return protoError("GOAWAY stream id must be zero, not: %d", f.StreamId())
	}
	f.debugToRead.R = rd
	f.debugToRead.N = int64(f.Length())
	return nil
}

func (f *GoAway) writeTo(wr io.Writer) (err error) {
	if err = f.common.writeTo(wr, goAwayFrameLength); err != nil {
		return
	}
	if _, err = wr.Write(f.debugToWrite); err != nil {
		return err
	}
	return
}

func (f *GoAway) Pack(lastStreamId StreamId, errCode ErrorCode, debug []byte) (err error) {
	if err = lastStreamId.valid(); err != nil {
		return
	}
	if err = f.common.pack(TypeGoAway, goAwayFrameLength+len(debug), 0, 0); err != nil {
		return
	}
	order.PutUint32(f.body(), uint32(lastStreamId))
	order.PutUint32(f.body()[4:], uint32(errCode))
	f.debugToWrite = debug
	return nil
}
