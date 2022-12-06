package proto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
)

func TestProtoMiddleware(t *testing.T) {
	opts := &HTTPEndpoint{}
	opts.BasicAuth = &pb.MiddlewareConfiguration_BasicAuth{
		Credentials: []*pb.MiddlewareConfiguration_BasicAuthCredential{{
			Username:          "foo",
			CleartextPassword: "bar",
		}},
	}

	opts.ProtoMiddleware = true

	jsonRepr, err := json.Marshal(opts)
	require.NoError(t, err)

	rawIsh := map[string]any{}

	err = json.Unmarshal(jsonRepr, &rawIsh)
	require.NoError(t, err)

	require.Contains(t, rawIsh, "MiddlewareBytes")
	require.NotEmpty(t, rawIsh["MiddlewareBytes"])

	unmarshalled := &HTTPEndpoint{}

	err = json.Unmarshal(jsonRepr, unmarshalled)
	require.NoError(t, err)

	require.NotEmpty(t, unmarshalled.BasicAuth)

	opts.ProtoMiddleware = false
	origRepr, err := json.Marshal(opts)
	require.NoError(t, err)

	rawIsh = map[string]any{}

	err = json.Unmarshal(origRepr, &rawIsh)
	require.NoError(t, err)

	require.Contains(t, rawIsh, "BasicAuth")
	require.NotEmpty(t, rawIsh["BasicAuth"].(map[string]any)["credentials"])
	require.Empty(t, rawIsh["MiddlewareBytes"])
}

func TestObfuscatedString(t *testing.T) {
	t.Parallel()

	fakeToken := "weeeeee"
	obfuscatedString := ObfuscatedString(fakeToken)
	require.Equal(t, fakeToken, obfuscatedString.PlainText())

	printedString := fmt.Sprintf("%s", obfuscatedString)
	require.NotEqual(t, fakeToken, printedString)
	require.Equal(t, "HIDDEN", printedString)
}
