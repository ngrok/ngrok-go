package proto

import (
	"encoding/json"
	"fmt"
)

// Unpack allows the caller to unpack anything that is specified
// in the protocol as an interface{}
// This includes the all Extra fields and the Options for a Bind.
// This is one of the most awful hacks in the whole library.
// The trouble is that anything that is passed as an empty interface in
// the protocol will be deserialized into a map[string]interface{}.
// So in order to get them in the form we want, we write them out
// as JSON again and then read them back in with the now-known
// proper type for deserializion
func Unpack(packed, unpacked any) error {
	bytes, err := json.Marshal(packed)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(bytes, unpacked); err != nil {
		return err
	}

	return nil
}

// Unpacks protocol options for Bind and BindResp messages. This is an internal
// function shared between client and server.
func UnpackProtoOpts(protocol string, opts any, bindMsg any) error {
	var unpackedOpts any

	switch protocol {
	case "http":
		unpackedOpts = &HTTPOptions{}
	case "https":
		unpackedOpts = &HTTPOptions{}
	case "tcp":
		unpackedOpts = &TCPOptions{}
	case "tls":
		unpackedOpts = &TLSOptions{}
	case "ssh":
		unpackedOpts = &SSHOptions{}
	default:
		return fmt.Errorf("invalid protocol: %s", protocol)
	}

	err := Unpack(opts, unpackedOpts)
	if err != nil {
		return err
	}

	switch msg := bindMsg.(type) {
	case *Bind:
		msg.Opts = unpackedOpts
	case *BindResp:
		msg.Opts = unpackedOpts
	default:
		return fmt.Errorf("unknown type for bindMsg: %#v", msg)
	}
	return nil
}
