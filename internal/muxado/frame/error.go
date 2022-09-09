package frame

import (
	"fmt"
)

type ErrorType int

const (
	ErrorFrameSize ErrorType = iota
	ErrorProtocol
	ErrorProtocolStream
)

type Error struct {
	errorType ErrorType
	error
}

func (e Error) Type() ErrorType {
	return e.errorType
}

func (e Error) Err() error {
	return e.error
}

func frameSizeError(length uint32, frameName string) error {
	return &Error{ErrorFrameSize, fmt.Errorf("illegal %s frame length: 0x%x", frameName, length)}
}

func protoError(fmtstr string, args ...interface{}) error {
	return &Error{ErrorProtocol, fmt.Errorf(fmtstr, args...)}
}

func protoStreamError(fmtstr string, args ...interface{}) error {
	return &Error{ErrorProtocolStream, fmt.Errorf(fmtstr, args...)}
}
