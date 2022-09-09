package frame

import (
	"fmt"
	"testing"
)

type wndIncTest struct {
	streamId         StreamId
	inc              uint32
	serialized       []byte
	serializeError   bool
	deserializeError bool
}

func (t *wndIncTest) FrameName() string         { return "WNDINC" }
func (t *wndIncTest) SerializeError() bool      { return t.serializeError }
func (t *wndIncTest) DeserializeError() bool    { return t.deserializeError }
func (t *wndIncTest) Serialized() []byte        { return t.serialized }
func (t *wndIncTest) WithHeader(c common) Frame { return &WndInc{common: c} }
func (t *wndIncTest) Pack() (Frame, error) {
	var f WndInc
	return &f, f.Pack(t.streamId, t.inc)
}
func (t *wndIncTest) Eq(fr Frame) error {
	f := fr.(*WndInc)
	if f.WindowIncrement() != t.inc {
		return fmt.Errorf("wrong increment. expected %v, got %v", t.inc, f.WindowIncrement())
	}
	return nil
}

func TestWndIncFrameValid(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &wndIncTest{
		streamId:         0x1,
		inc:              0x12c498,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeWndInc << 4), 0, 0, 0, 0x1, 0x00, 0x12, 0xc4, 0x98},
		serializeError:   false,
		deserializeError: false,
	})
	RunFrameTest(t, &wndIncTest{
		streamId:         0x1,
		inc:              wndIncMask,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeWndInc << 4), 0, 0, 0, 0x1, 0x7f, 0xff, 0xff, 0xff},
		serializeError:   false,
		deserializeError: false,
	})
}

func TestWndIncZeroIncrement(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &wndIncTest{
		streamId:         0x04b1bd09,
		inc:              0x0,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeWndInc << 4), 0x04, 0xb1, 0xbd, 0x09, 0x0, 0x0, 0x0, 0x0},
		serializeError:   false,
		deserializeError: true,
	})
}

// test a bad frame length of wndIncBodySize+1
func TestBadLengthWndInc(t *testing.T) {
	t.Parallel()

	RunFrameTest(t, &wndIncTest{
		streamId:         0x04b1bd09,
		inc:              0x1,
		serialized:       []byte{0x0, 0x0, 0x5, byte(TypeWndInc << 4), 0x04, 0xb1, 0xbd, 0x09, 0x0, 0x0, 0x0, 0x1, 0x0},
		serializeError:   false,
		deserializeError: true,
	})
}

// test fewer than rstBodySize bytes available after header
func TestShortReadWndInc(t *testing.T) {
	t.Parallel()

	RunFrameTest(t, &wndIncTest{
		streamId:         0x04b1bd09,
		inc:              0x1,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeWndInc << 4), 0x04, 0xb1, 0xbd, 0x09, 0x0, 0x0, 0x1},
		serializeError:   false,
		deserializeError: true,
	})
}
