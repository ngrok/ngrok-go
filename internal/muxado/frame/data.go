package frame

import (
	"io"
)

type Data struct {
	common

	toRead  io.LimitedReader // when reading, the underlying io.Reader is handed up
	toWrite []byte           // when writing, these are the bytes to write
}

func (f *Data) Fin() bool {
	return f.flags.IsSet(FlagDataFin)
}

func (f *Data) Syn() bool {
	return f.flags.IsSet(FlagDataSyn)
}

func (f *Data) Reader() io.Reader {
	return &f.toRead
}

// Returns the data this
func (f *Data) Bytes() []byte {
	return f.toWrite
}

func (f *Data) readFrom(rd io.Reader) error {
	if f.StreamId() == 0 {
		return protoError("DATA frame stream id must not be zero, got: %d", f.StreamId())
	}
	// not using io.LimitReader to avoid a heap memory allocation in the hot path
	f.toRead.R = rd
	f.toRead.N = int64(f.Length())
	return nil
}

func (f *Data) writeTo(wr io.Writer) (err error) {
	if err = f.common.writeTo(wr, 0); err != nil {
		return err
	}
	if _, err = wr.Write(f.toWrite); err != nil {
		return err
	}
	return
}

func (f *Data) Pack(streamId StreamId, data []byte, fin bool, syn bool) (err error) {
	var flags Flags
	if fin {
		flags.Set(FlagDataFin)
	}
	if syn {
		flags.Set(FlagDataSyn)
	}
	if err = f.common.pack(TypeData, len(data), streamId, flags); err != nil {
		return
	}
	f.toWrite = data
	return
}
