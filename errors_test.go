package ngrok

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// Sanity check for the approach to error construction/wrapping
func TestErrorWrapping(t *testing.T) {
	inner := errors.New("testing, 1 2 3")
	accept := errAcceptFailed{Inner: inner}
	auth := errAuthFailed{true, accept}

	require.ErrorIs(t, accept, errAcceptFailed{})
	require.ErrorIs(t, auth, errAuthFailed{})
	require.ErrorIs(t, auth, errAcceptFailed{})

	var downcastAuth errAuthFailed
	var downcastAccept errAcceptFailed

	require.ErrorAs(t, auth, &downcastAuth)
	require.ErrorAs(t, auth, &downcastAccept)

	require.ErrorAs(t, accept, &downcastAccept)

	require.True(t, downcastAuth.Remote)
}

func TestNgrokErrorWrapping(t *testing.T) {
	rootErr := proto.StringError("ngrok error\n\nERR_NGROK_123")
	nonNgrokRootErr := errors.New("generic non ngrok error")

	ngrokErr := errAuthFailed{true, rootErr}
	nonNgrokErr := errAuthFailed{true, nonNgrokRootErr}

	require.EqualError(t, ngrokErr, "authentication failed: ngrok error\n\nERR_NGROK_123")

	var nerr Error
	require.ErrorAs(t, ngrokErr, &nerr)

	require.EqualError(t, nerr, "ngrok error\n\nERR_NGROK_123")
	require.Equal(t, "ngrok error", nerr.Msg())
	require.Equal(t, "ERR_NGROK_123", nerr.ErrorCode())

	require.False(t, errors.As(nonNgrokErr, &nerr))
}
