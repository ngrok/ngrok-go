package ngrok

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var testError = errors.New("testing, 1 2 3!")

// Sanity check for the appraoch to error construction/wrapping
func TestErrorWrapping(t *testing.T) {
	var accept error = ErrAcceptFailed{Inner: testError}
	var auth error = ErrAuthFailed{true, accept}

	require.True(t, errors.Is(accept, ErrAcceptFailed{}))
	require.True(t, errors.Is(auth, ErrAuthFailed{}))
	require.True(t, errors.Is(auth, ErrAcceptFailed{}))

	var downcastAuth ErrAuthFailed
	var downcastAccept ErrAcceptFailed

	require.True(t, errors.As(auth, &downcastAuth))
	require.True(t, errors.As(auth, &downcastAccept))

	require.True(t, errors.As(accept, &downcastAccept))

	require.True(t, downcastAuth.Remote)
}
