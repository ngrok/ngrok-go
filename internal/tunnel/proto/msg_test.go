package proto

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestLogValueRedactsSecrets(t *testing.T) {
	t.Parallel()

	const (
		credential = "fake-credential"
		cookie     = "fake-cookie"
		bindCred   = "fake-bind-credential"
	)

	cases := []struct {
		name    string
		msg     any
		secrets []string
	}{
		{
			name:    "ObfuscatedString",
			msg:     ObfuscatedString(credential),
			secrets: []string{credential},
		},
		{
			name: "Auth",
			msg: &Auth{Extra: AuthExtra{
				Authtoken: ObfuscatedString(credential),
				Cookie:    cookie,
			}},
			secrets: []string{credential, cookie},
		},
		{
			name:    "AuthResp",
			msg:     &AuthResp{Extra: AuthRespExtra{Cookie: cookie}},
			secrets: []string{cookie},
		},
		{
			name:    "Bind",
			msg:     &Bind{Extra: BindExtra{Token: bindCred}},
			secrets: []string{bindCred},
		},
		{
			name:    "BindResp",
			msg:     &BindResp{Extra: BindRespExtra{Token: bindCred}},
			secrets: []string{bindCred},
		},
	}

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
	}

	for _, tc := range cases {
		for handlerName, newHandler := range handlers {
			t.Run(tc.name+"/"+handlerName, func(t *testing.T) {
				t.Parallel()

				var buf bytes.Buffer
				slog.New(newHandler(&buf)).Info("encode request", "req", tc.msg)

				out := buf.String()
				require.Contains(t, out, "HIDDEN")
				for _, secret := range tc.secrets {
					require.NotContains(t, out, secret)
				}
			})
		}

		// The wire protocol JSON-encodes these messages, so redaction must
		// never leak into their JSON encoding.
		t.Run(tc.name+"/wire", func(t *testing.T) {
			t.Parallel()

			wire, err := json.Marshal(tc.msg)
			require.NoError(t, err)
			for _, secret := range tc.secrets {
				require.Contains(t, string(wire), secret)
			}
		})
	}
}

func TestLogValueLeavesEmptySecretsEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("encode request", "req", &Auth{})
	require.NotContains(t, buf.String(), "HIDDEN")
}
