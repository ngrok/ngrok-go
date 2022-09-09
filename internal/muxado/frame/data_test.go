package frame

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

type dataTest struct {
	streamId       StreamId
	data           []byte
	fin            bool
	serialized     []byte
	serializeError bool
}

func (t *dataTest) FrameName() string         { return "DATA" }
func (t *dataTest) SerializeError() bool      { return t.serializeError }
func (t *dataTest) DeserializeError() bool    { return false }
func (t *dataTest) Serialized() []byte        { return t.serialized }
func (t *dataTest) WithHeader(c common) Frame { return &Data{common: c} }
func (t *dataTest) Pack() (Frame, error) {
	var f Data
	return &f, f.Pack(t.streamId, t.data, t.fin, false)
}
func (dt *dataTest) Eq(fr Frame) error {
	f := fr.(*Data)
	switch {
	case dt.fin && !f.Fin():
		return fmt.Errorf("expected fin flag but it was not set: %+v", dt)
	case !dt.fin && f.Fin():
		return fmt.Errorf("unexpected fin flag: %+v", dt)
	}
	buf, err := ioutil.ReadAll(f.Reader())
	if err != nil {
		return fmt.Errorf("Failed to read data: %v, %+v", err, dt)
	}
	if !bytes.Equal(dt.data, buf) {
		return fmt.Errorf("data read does not match expected. got: %x, expected: %x", dt.data, buf)
	}
	return nil
}

func TestDataFrameValid(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &dataTest{
		streamId:       0x49a1bb00,
		data:           []byte{0x00, 0x01, 0x02, 0x03, 0x04},
		fin:            false,
		serialized:     []byte{0, 0, 0x5, byte(TypeData << 4), 0x49, 0xa1, 0xbb, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04},
		serializeError: false,
	})
}

func TestDataFrameFin(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &dataTest{
		streamId:       streamMask,
		data:           []byte{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00},
		fin:            true,
		serialized:     []byte{0x00, 0x0, 0x10, byte((TypeData << 4) | FlagDataFin), 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00},
		serializeError: false,
	})
}

func TestDataFrameZeroLength(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &dataTest{
		streamId:       0x1,
		data:           []byte{},
		fin:            false,
		serialized:     []byte{0x0, 0x0, 0x0, byte(TypeData << 4), 0x0, 0x0, 0x0, 0x1},
		serializeError: false,
	})
}

func TestDataFrameTooLong(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &dataTest{
		streamId:       0x0,
		data:           make([]byte, lengthMask+1),
		fin:            false,
		serialized:     []byte{},
		serializeError: true,
	})
}

func TestDataFrameReadLengthLimited(t *testing.T) {
	t.Parallel()

	dt := &dataTest{
		streamId:       0x49a1bb00,
		data:           []byte{0x00, 0x01, 0x02, 0x03, 0x04},
		fin:            false,
		serialized:     []byte{0, 0, 0x5, byte(TypeData << 4), 0x49, 0xa1, 0xbb, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04},
		serializeError: false,
	}
	buf := bytes.NewBuffer(dt.serialized)
	buf.Write([]byte("extra data that shouldn't be read"))
	var f *Data = new(Data)
	if err := f.common.readFrom(buf); err != nil {
		t.Fatalf("failed read frame header: %v, %+v", err, dt)
	}
	err := f.readFrom(buf)
	if err != nil {
		t.Fatalf("failed to read data frame: %v", err)
	}
	if err := dt.Eq(f); err != nil {
		t.Fatalf(err.Error())
	}
}
