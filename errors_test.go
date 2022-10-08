package ngrok

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var testError = errors.New("testing, 1 2 3!")

// Sanity check for the appraoch to error construction/wrapping
func TestErrorWrapping(t *testing.T) {
	var accept error = errAcceptFailed{Inner: testError}
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
