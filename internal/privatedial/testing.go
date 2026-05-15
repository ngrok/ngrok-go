package privatedial

import (
	"io"

	"google.golang.org/protobuf/proto"
)

// ReadFrameForTest exposes the package-private readFrame to test code in
// other packages (e.g. the public-surface tests).
func ReadFrameForTest(r io.Reader, msg proto.Message) error {
	return readFrame(r, msg)
}

// WriteFrameForTest exposes the package-private writeFrame to test code in
// other packages.
func WriteFrameForTest(w io.Writer, msg proto.Message) error {
	return writeFrame(w, msg)
}
