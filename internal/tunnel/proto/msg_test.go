package proto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObfuscatedString(t *testing.T) {
	t.Parallel()

	fakeToken := "weeeeee"
	obfuscatedString := ObfuscatedString(fakeToken)
	require.Equal(t, fakeToken, obfuscatedString.PlainText())

	printedString := fmt.Sprintf("%s", obfuscatedString)
	require.NotEqual(t, fakeToken, printedString)
	require.Equal(t, "HIDDEN", printedString)
}

const (
	credential = "fake-credential"
	cookie     = "fake-cookie"
	bindCred   = "fake-bind-credential"
	// Fake traffic policy secrets, one per masked field.
	password   = "s3cr3t-test-password"
	bareCred   = "s3cr3t-test-bare-credential"
	oauthCred  = "s3cr3t-test-oauth-client-secret"
	oidcCred   = "s3cr3t-test-oidc-client-secret"
	hookCred   = "s3cr3t-test-webhook-secret"
	tlsKey     = "s3cr3t-test-server-private-key"
	tlsKey2    = "s3cr3t-test-private-key"
	numberCred = "7346591827"
	deepCred   = "s3cr3t-test-nested-secret"
)

const (
	basicAuthYAML = "on_http_request:\n  - actions:\n      - type: basic-auth\n        config:\n          credentials:\n" +
		"            - alice:" + password + "\n            - " + bareCred + "\n"
	oauthJSON  = `{"on_http_request":[{"actions":[{"type":"oauth","config":{"provider":"google","auth":{"credentials":{"client_id":"visible-client-id","client_secret":"` + oauthCred + `"}}}}]}]}`
	oidcJSON   = `{"on_http_request":[{"actions":[{"type":"openid-connect","config":{"client_id":"visible-client-id","client_secret":"` + oidcCred + `"}}]}]}`
	hookYAML   = "on_http_request:\n  - actions:\n      - type: verify-webhook\n        config:\n          provider: github\n          secret: " + hookCred + "\n"
	tlsYAML    = "on_tcp_connect:\n  - actions:\n      - type: terminate-tls\n        config:\n          server_certificate: visible-cert\n          server_private_key: " + tlsKey + "\n          private_key: " + tlsKey2 + "\n"
	numberYAML = "on_http_request:\n  - actions:\n      - type: openid-connect\n        config:\n          client_id: visible-client-id\n          client_secret: " + numberCred + "\n"
	deepJSON   = `{"on_http_request":[{"expressions":["visible-expression"],"actions":[{"type":"custom","config":{"a":[{"b":{"secret":"` + deepCred + `"}}]}}]}]}`
)

// decodedOpts builds the map a BindResp's Opts holds when the response is
// logged: the JSON decoding of the server's options.
func decodedOpts(policy any) map[string]any {
	return map[string]any{"URL": "https://visible.example.com", "TrafficPolicy": policy}
}

func policyMap() map[string]any {
	return map[string]any{"on_http_request": []any{map[string]any{"actions": []any{map[string]any{
		"type":   "verify-webhook",
		"config": map[string]any{"provider": "github", "secret": hookCred},
	}}}}}
}

type logValueCase struct {
	name string
	// newMsg builds a fresh message per subtest, so parallel subtests never
	// share one and a LogValue that redacts in place cannot hide behind a
	// copy made by another subtest.
	newMsg  func() any
	secrets []string
	// visible holds non-secret field values that must survive redaction. The
	// debug lines these messages appear in exist to be read, so replacing a
	// whole message with HIDDEN is as wrong as leaking the secret.
	visible []string
	// pointerPolicy marks a policy the text handler prints as a pointer, so
	// that output carries no mask.
	pointerPolicy bool
}

func logValueCases() []logValueCase {
	return []logValueCase{
		{
			name:    "ObfuscatedString",
			newMsg:  func() any { return ObfuscatedString(credential) },
			secrets: []string{credential},
		},
		{
			name: "Auth",
			newMsg: func() any {
				return &Auth{
					ClientID: "visible-client-id",
					Extra: AuthExtra{
						Authtoken: ObfuscatedString(credential),
						Cookie:    cookie,
						Metadata:  "visible-metadata",
					},
				}
			},
			secrets: []string{credential, cookie},
			visible: []string{"visible-client-id", "visible-metadata"},
		},
		{
			name: "AuthResp",
			newMsg: func() any {
				return &AuthResp{
					ClientID: "visible-client-id",
					Extra: AuthRespExtra{
						Cookie: cookie,
						Region: "visible-region",
					},
				}
			},
			secrets: []string{cookie},
			visible: []string{"visible-client-id", "visible-region"},
		},
		{
			name: "Bind",
			newMsg: func() any {
				return &Bind{
					ClientID:   "visible-client-id",
					ForwardsTo: "visible-forwards-to",
					Extra:      BindExtra{Token: bindCred},
				}
			},
			secrets: []string{bindCred},
			visible: []string{"visible-client-id", "visible-forwards-to"},
		},
		{
			name: "BindResp",
			newMsg: func() any {
				return &BindResp{
					ClientID: "visible-client-id",
					URL:      "https://visible.example.com",
					Extra:    BindRespExtra{Token: bindCred, TunnelID: "visible-tunnel-id"},
				}
			},
			secrets: []string{bindCred},
			visible: []string{"visible-client-id", "https://visible.example.com", "visible-tunnel-id"},
		},
		{
			name:    "BindResp/basic-auth",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(basicAuthYAML)} },
			secrets: []string{password, bareCred},
			visible: []string{"TrafficPolicy", "https://visible.example.com", "alice:" + secretMask, "basic-auth"},
		},
		{
			name:    "BindResp/oauth",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(oauthJSON)} },
			secrets: []string{oauthCred},
			visible: []string{"visible-client-id", secretMask},
		},
		{
			name:    "BindResp/oidc",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(oidcJSON)} },
			secrets: []string{oidcCred},
			visible: []string{"visible-client-id", secretMask},
		},
		{
			name:    "BindResp/verify-webhook",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(hookYAML)} },
			secrets: []string{hookCred},
			visible: []string{"github", secretMask},
		},
		{
			name:    "BindResp/numeric secret",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(numberYAML)} },
			secrets: []string{numberCred},
			visible: []string{"visible-client-id", secretMask},
		},
		{
			name:    "BindResp/nested secret",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(deepJSON)} },
			secrets: []string{deepCred},
			visible: []string{"visible-expression", secretMask},
		},
		{
			name:    "BindResp/map policy",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(policyMap())} },
			secrets: []string{hookCred},
			visible: []string{"github", secretMask},
		},
		{
			name:    "BindResp/unparsable policy",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts("on_http_request: [\n  secret: " + hookCred)} },
			secrets: []string{hookCred},
			visible: []string{"TrafficPolicy", "https://visible.example.com"},
		},
		{
			name: "BindResp/JSON escape YAML rejects",
			newMsg: func() any {
				return &BindResp{Opts: decodedOpts(`{"on_http_request":[{"actions":[{"type":"verify-webhook","config":{"secret":"h\/` + hookCred + `"}}]}]}`)}
			},
			secrets: []string{hookCred},
			visible: []string{"verify-webhook", secretMask},
		},
		{
			name: "BindResp/non-string keys",
			newMsg: func() any {
				return &BindResp{Opts: decodedOpts("on_http_request:\n  - actions:\n      - config:\n          1: visible\n          secret: " + hookCred + "\n")}
			},
			secrets: []string{hookCred},
			visible: []string{"TrafficPolicy"},
		},
		{
			name:    "BindResp/scalar policy",
			newMsg:  func() any { return &BindResp{Opts: decodedOpts(password)} },
			secrets: []string{password},
			visible: []string{"TrafficPolicy"},
		},
		{
			name: "Bind/terminate-tls",
			newMsg: func() any {
				return &Bind{Proto: "tls", Opts: &TLSEndpoint{URL: "tls://visible.example.com", TrafficPolicy: tlsYAML}}
			},
			secrets: []string{tlsKey, tlsKey2},
			visible: []string{"tls://visible.example.com", "visible-cert", secretMask},
		},
		{
			name: "Bind/http",
			newMsg: func() any {
				return &Bind{Proto: "https", Opts: &HTTPEndpoint{URL: "https://visible.example.com", TrafficPolicy: oidcJSON}}
			},
			secrets: []string{oidcCred},
			visible: []string{"https://visible.example.com", secretMask},
		},
		{
			name: "PatchTunnelState",
			newMsg: func() any {
				policy := basicAuthYAML
				return &PatchTunnelState{TunnelID: "visible-tunnel-id", TrafficPolicy: &policy}
			},
			secrets:       []string{password, bareCred},
			visible:       []string{"visible-tunnel-id"},
			pointerPolicy: true,
		},
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
	}

	for _, tc := range logValueCases() {
		for handlerName, newHandler := range handlers {
			t.Run(tc.name+"/"+handlerName, func(t *testing.T) {
				t.Parallel()

				var buf bytes.Buffer
				slog.New(newHandler(&buf)).Info("encode request", "req", tc.newMsg())

				out := buf.String()
				// Also catches a LogValue that resolves to itself: slog gives
				// up after too many resolutions and replaces the value with an
				// error string, leaving no mask behind.
				if !tc.pointerPolicy || handlerName != "text" {
					require.Regexp(t, `HIDDEN|\*{8}`, out)
				}
				for _, secret := range tc.secrets {
					require.NotContains(t, out, secret)
				}
				for _, field := range tc.visible {
					require.Contains(t, out, field)
				}
			})
		}

		// The wire protocol JSON-encodes these messages, so redaction must
		// never leak into their JSON encoding.
		t.Run(tc.name+"/wire", func(t *testing.T) {
			t.Parallel()

			wire, err := json.Marshal(tc.newMsg())
			require.NoError(t, err)
			for _, secret := range tc.secrets {
				require.Contains(t, string(wire), secret)
			}
		})
	}
}

// TestLogValueDoesNotMutateMessage pins the reason LogValue takes a value
// receiver. rawSession.rpc logs the same value it sends, so redacting in place
// would put HIDDEN on the wire as the credential.
func TestLogValueDoesNotMutateMessage(t *testing.T) {
	t.Parallel()

	for _, tc := range logValueCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := tc.newMsg()
			slog.New(slog.NewJSONHandler(io.Discard, nil)).Info("encode request", "req", msg)
			require.Equal(t, tc.newMsg(), msg)
		})
	}
}

func TestLogValueLeavesEmptySecretsEmpty(t *testing.T) {
	t.Parallel()

	msgs := map[string]any{
		"Auth":                  &Auth{},
		"AuthResp":              &AuthResp{},
		"Bind":                  &Bind{},
		"BindResp":              &BindResp{},
		"BindResp/empty policy": &BindResp{Opts: decodedOpts("")},
		"Bind/empty policy":     &Bind{Opts: &HTTPEndpoint{}},
		"PatchTunnelState":      &PatchTunnelState{TrafficPolicy: new(string)},
		"BindResp/empty secret": &BindResp{Opts: decodedOpts(`{"on_http_request":[{"actions":[{"type":"verify-webhook","config":{"secret":""}}]}]}`)},
	}

	for name, msg := range msgs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("encode request", "req", msg)
			require.NotContains(t, buf.String(), "HIDDEN")
			require.NotContains(t, buf.String(), secretMask)
		})
	}
}
