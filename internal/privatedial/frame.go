package privatedial

import (
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

// readFrame reads a length-prefixed protobuf message. The on-wire format is a
// uint16 little-endian length followed by that many bytes of marshalled
// protobuf. Mirrors the gateway's libmux.ReadProxyMessage.
func readFrame(r io.Reader, msg proto.Message) error {
	var hdrLen uint16
	if err := binary.Read(r, binary.LittleEndian, &hdrLen); err != nil {
		return fmt.Errorf("read frame length: %w", err)
	}
	buf := make([]byte, hdrLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("read frame body: %w", err)
	}
	if err := proto.Unmarshal(buf, msg); err != nil {
		return fmt.Errorf("unmarshal frame: %w", err)
	}
	return nil
}

// writeFrame writes a length-prefixed protobuf message in the same format.
func writeFrame(w io.Writer, msg proto.Message) error {
	buf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	if len(buf) > 0xFFFF {
		return fmt.Errorf("frame too large: %d bytes", len(buf))
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(len(buf))); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}
