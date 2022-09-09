package muxado

import (
	"encoding/binary"
	"net"
)

var order = binary.BigEndian

type StreamType uint32

type TypedStream interface {
	Stream
	StreamType() StreamType
}

type TypedStreamSession interface {
	Session
	OpenTypedStream(stype StreamType) (Stream, error)
	AcceptTypedStream() (TypedStream, error)
}

func NewTypedStreamSession(s Session) TypedStreamSession {
	return &typedStreamSession{s}
}

type typedStreamSession struct {
	Session
}

func (s *typedStreamSession) Accept() (net.Conn, error) {
	return s.AcceptStream()
}

func (s *typedStreamSession) AcceptStream() (Stream, error) {
	return s.AcceptTypedStream()
}

func (s *typedStreamSession) AcceptTypedStream() (TypedStream, error) {
	str, err := s.Session.AcceptStream()
	if err != nil {
		return nil, err
	}
	var stype [4]byte
	_, err = str.Read(stype[:])
	if err != nil {
		str.Close()
		return nil, err
	}
	return &typedStream{str, StreamType(order.Uint32(stype[:]))}, nil
}

func (s *typedStreamSession) OpenTypedStream(st StreamType) (Stream, error) {
	str, err := s.Session.OpenStream()
	if err != nil {
		return nil, err
	}
	var stype [4]byte
	order.PutUint32(stype[:], uint32(st))
	_, err = str.Write(stype[:])
	if err != nil {
		return nil, err
	}
	return &typedStream{str, st}, nil
}

type typedStream struct {
	Stream
	streamType StreamType
}

func (s *typedStream) StreamType() StreamType {
	return s.streamType
}
