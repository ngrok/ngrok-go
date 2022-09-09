package frame

import (
	"bytes"
	"testing"
)

type packTest struct {
	ftype          Type
	length         int
	streamId       StreamId
	flags          Flags
	serialized     []byte
	serializeError bool
}

func (pt *packTest) eq(t *testing.T, c common) {
	if c.Type() != pt.ftype {
		t.Errorf("Failed deserialization. Expected type %x, got: %x", pt.ftype, c.Type())
		return
	}
	if c.Length() != uint32(pt.length&lengthMask) && (pt.length < 0 && c.Length() != 0) {
		t.Errorf("Failed deserialization. Expected length %x, got: %x", pt.length, c.Length())
		return
	}
	if c.Flags() != pt.flags {
		t.Errorf("Failed deserialization. Expected flags %x, got: %x", pt.flags, c.Flags())
		return
	}
	if c.StreamId() != pt.streamId {
		t.Errorf("Failed deserialization. Expected stream id %x, got: %x", pt.streamId, c.StreamId())
		return
	}
}

func TestPack(t *testing.T) {
	t.Parallel()
	tests := []packTest{
		packTest{
			ftype:          TypeRst,
			length:         0x4,
			streamId:       0x2843,
			flags:          0,
			serialized:     []byte{0, 0, 0x4, 0x00, 0, 0, 0x28, 0x43},
			serializeError: false,
		},
		packTest{
			ftype:          0x7,
			length:         0x37BD,
			streamId:       0x0,
			flags:          0x2,
			serialized:     []byte{0x00, 0x37, 0xBD, 0x72, 0, 0, 0, 0},
			serializeError: false,
		},
		packTest{
			ftype:          0,
			length:         0,
			streamId:       0,
			flags:          0,
			serialized:     []byte{0, 0, 0, 0, 0, 0, 0, 0},
			serializeError: false,
		},
		packTest{
			ftype:          0xF,
			length:         lengthMask,
			streamId:       streamMask,
			flags:          flagsMask,
			serialized:     []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF},
			serializeError: false,
		},
		packTest{
			ftype:          0xC,
			length:         0x0F1DAA,
			streamId:       0x4F224719,
			flags:          0xF,
			serialized:     []byte{0x0F, 0x1D, 0xAA, 0xCF, 0x4F, 0x22, 0x47, 0x19},
			serializeError: false,
		},
		packTest{
			ftype:          0xC,
			length:         0x0F1DAA,
			streamId:       streamMask + 1,
			flags:          0xF,
			serialized:     []byte{0x0F, 0x1D, 0xAA, 0xCF, 0x80, 0x00, 0x00, 0x00},
			serializeError: true,
		},
		packTest{
			ftype:          0x0,
			length:         0x000000,
			streamId:       0xFFFFFFFF,
			flags:          0x0,
			serialized:     []byte{0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF},
			serializeError: true,
		},
		packTest{
			ftype:          0x0,
			length:         lengthMask + 1,
			streamId:       0x1,
			flags:          0x0,
			serialized:     []byte{0, 0, 0, 0, 0, 0, 0, 0x01},
			serializeError: true,
		},
		packTest{
			ftype:          0x0,
			length:         -1,
			streamId:       0x1,
			flags:          0x0,
			serialized:     []byte{0, 0, 0, 0, 0, 0, 0, 0x01},
			serializeError: true,
		},
	}

	// test serialization
	for _, pt := range tests {
		var c common
		err := c.pack(pt.ftype, pt.length, pt.streamId, pt.flags)
		switch {
		case err != nil && !pt.serializeError:
			t.Errorf("Unexpected error packing header: %v, %+v", err, pt)
			continue
		case err == nil && pt.serializeError:
			t.Errorf("Expected error packing header, but was successful: %+v", pt)
			continue
		}
	}

	// test deserialization
	for _, pt := range tests {
		var c common
		err := c.readFrom(bytes.NewReader(pt.serialized))
		if err != nil {
			t.Errorf("Header readFrom should never return an error, but failed with: %v, %+v", err, pt)
			continue
		}
		pt.eq(t, c)
	}

	// test serialization round-trip
	for _, pt := range tests {
		if pt.serializeError {
			// skip test
			continue
		}
		var c common
		err := c.pack(pt.ftype, pt.length, pt.streamId, pt.flags)
		if err != nil {
			t.Errorf("Failed to round-trip pack: %v, %+v", err, pt)
		}
		var b bytes.Buffer
		err = c.writeTo(&b, 0)
		if err != nil {
			t.Errorf("Failed to round-trip serialize: %v, %+v", err, pt)
			continue
		}
		err = c.readFrom(&b)
		if err != nil {
			t.Errorf("Failed to round-trip deserialize: %v, %+v", err, pt)
			continue
		}
		pt.eq(t, c)
	}
}
