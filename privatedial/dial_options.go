package privatedial

import (
	"maps"

	pbpd "golang.ngrok.com/ngrok/privatedial/internal/pb_private_dial"
)

// PodIdentity names the Kubernetes pod that opened the connection a caller
// forwards through DialContextWithOptions.
//
// The server exposes it to Traffic Policy on the endpoint as the
// conn.k8s.pod.* variables. The server does not verify it. Any credential
// that can dial in the account can set any identity, so do not use it as an
// authentication signal.
type PodIdentity struct {
	UID         string
	Name        string
	Namespace   string
	Annotations map[string]string
}

// DialOption is a functional option for DialContextWithOptions.
type DialOption func(*dialOpts)

type dialOpts struct {
	podIdentity *pbpd.PodIdentity
}

// WithPodIdentity sends id to the server with the dial.
//
// A zero PodIdentity is still sent. It tells the server that the caller
// looked for a pod and found none. To send no identity, omit this option.
func WithPodIdentity(id PodIdentity) DialOption {
	// Copy the map so that later changes by the caller do not race with
	// the marshal of the dial request.
	pb := &pbpd.PodIdentity{
		Uid:         id.UID,
		Name:        id.Name,
		Namespace:   id.Namespace,
		Annotations: maps.Clone(id.Annotations),
	}
	return func(o *dialOpts) {
		o.podIdentity = pb
	}
}
