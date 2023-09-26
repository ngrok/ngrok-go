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
	var accept error = errAcceptFailed{Inner: inner}
	var auth error = errAuthFailed{true, accept}

	require.True(t, errors.Is(accept, errAcceptFailed{}))
	require.True(t, errors.Is(auth, errAuthFailed{}))
	require.True(t, errors.Is(auth, errAcceptFailed{}))

	var downcastAuth errAuthFailed
	var downcastAccept errAcceptFailed

	require.True(t, errors.As(auth, &downcastAuth))
	require.True(t, errors.As(auth, &downcastAccept))

	require.True(t, errors.As(accept, &downcastAccept))

	require.True(t, downcastAuth.Remote)
}

func TestNgrokErrorWrapping(t *testing.T) {
	rootErr := proto.StringError("ngrok error\n\nERR_NGROK_123")
	nonNgrokRootErr := errors.New("generic non ngrok error")

	ngrokErr := errAuthFailed{true, rootErr}
	nonNgrokErr := errAuthFailed{true, nonNgrokRootErr}

	require.Equal(t, ngrokErr.Error(), "authentication failed: ngrok error\n\nERR_NGROK_123")

	var nerr Error
	require.True(t, errors.As(ngrokErr, &nerr))

	require.Equal(t, nerr.Error(), "ngrok error\n\nERR_NGROK_123")
	require.Equal(t, nerr.Msg(), "ngrok error")
	require.Equal(t, nerr.ErrorCode(), "ERR_NGROK_123")

	require.False(t, errors.As(nonNgrokErr, &nerr))
}
