package frame

import "io"

type Unknown struct {
	common
	toRead io.LimitedReader
}

func (f *Unknown) PayloadReader() io.Reader {
	return &f.toRead
}

func (f *Unknown) readFrom(rd io.Reader) error {
	f.toRead.R = rd
	f.toRead.N = int64(f.Length())
	return nil
}

func (f *Unknown) writeTo(wr io.Writer) (err error) {
	panic("cannot write unknown frame")
}
