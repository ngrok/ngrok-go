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
)

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
				// error string, leaving no HIDDEN behind.
				require.Contains(t, out, "HIDDEN")
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
		"Auth":     &Auth{},
		"AuthResp": &AuthResp{},
		"Bind":     &Bind{},
		"BindResp": &BindResp{},
	}

	for name, msg := range msgs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("encode request", "req", msg)
			require.NotContains(t, buf.String(), "HIDDEN")
		})
	}
}
