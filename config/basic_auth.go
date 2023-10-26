package config

import "golang.ngrok.com/ngrok/internal/pb"

// BasicAuth is a set of credentials for basic authentication.
type basicAuth struct {
	// The username for basic authentication.
	Username string
	// The password for basic authentication.
	// Must be at least eight characters.
	Password string
}

func (ba basicAuth) toProtoConfig() *pb.MiddlewareConfiguration_BasicAuthCredential {
	return &pb.MiddlewareConfiguration_BasicAuthCredential{
		CleartextPassword: ba.Password,
		Username:          ba.Username,
	}
}

// WithBasicAuth adds the provided credentials to the list of basic
// authentication credentials.
//
// https://ngrok.com/docs/http/basic-auth/
func WithBasicAuth(username, password string) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.BasicAuth = append(cfg.BasicAuth,
			basicAuth{
				Username: username,
				Password: password,
			})
	})
}
