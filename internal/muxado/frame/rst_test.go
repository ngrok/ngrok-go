package frame

import (
	"fmt"
	"testing"
)

type rstTest struct {
	streamId         StreamId
	errorCode        ErrorCode
	serialized       []byte
	serializeError   bool
	deserializeError bool
}

func (r *rstTest) FrameName() string         { return "RST" }
func (r *rstTest) SerializeError() bool      { return r.serializeError }
func (r *rstTest) DeserializeError() bool    { return r.deserializeError }
func (r *rstTest) Serialized() []byte        { return r.serialized }
func (r *rstTest) WithHeader(c common) Frame { return &Rst{common: c} }
func (r *rstTest) Pack() (Frame, error) {
	var f Rst
	return &f, f.Pack(r.streamId, r.errorCode)
}
func (r *rstTest) Eq(f Frame) error {
	fr, ok := f.(*Rst)
	if !ok {
		return fmt.Errorf("wrong frame type, expected RST!")
	}
	if r.errorCode != fr.ErrorCode() {
		return fmt.Errorf("expected error code %v but got %v", r.errorCode, fr.ErrorCode())
	}
	return nil
}

func TestValidRstFrames(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &rstTest{
		streamId:         0x49a1bb00,
		errorCode:        0x5,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeRst << 4), 0x49, 0xa1, 0xbb, 0x00, 0x0, 0x0, 0x0, 0x5},
		serializeError:   false,
		deserializeError: false,
	})
	RunFrameTest(t, &rstTest{
		streamId:         0x49a1bb00,
		errorCode:        0x0,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeRst << 4), 0x49, 0xa1, 0xbb, 0x00, 0x0, 0x0, 0x0, 0x0},
		serializeError:   false,
		deserializeError: false,
	})
}

func TestBadRstFrameLength(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &rstTest{
		streamId:         0x49a1bb00,
		errorCode:        0x0,
		serialized:       []byte{0x0, 0x0, 0x3, byte(TypeRst << 4), 0x49, 0xa1, 0xbb, 0x00, 0x0, 0x0, 0x0},
		serializeError:   false,
		deserializeError: true,
	})
}

func TestRstZeroStream(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &rstTest{
		streamId:         0x0,
		errorCode:        0x1,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeRst << 4), 0, 0, 0, 0, 0x0, 0x0, 0x0, 0x1},
		serializeError:   false,
		deserializeError: true,
	})
}

func TestRstShortPayload(t *testing.T) {
	t.Parallel()
	RunFrameTest(t, &rstTest{
		streamId:         0x49a1bb00,
		errorCode:        0x0,
		serialized:       []byte{0x0, 0x0, 0x4, byte(TypeRst << 4), 0x49, 0xa1, 0xbb, 0x00, 0x0, 0x0, 0x1},
		serializeError:   false,
		deserializeError: true,
	})
}
