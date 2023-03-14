package ngrok

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserAgent(t *testing.T) {
	s := (&clientInfo{"library/official/go", "1.2.3"}).ToUserAgent()
	require.Equal(t, s, "library-official-go/1.2.3")

	s = (&clientInfo{"some@funky☺user agent", "№1.2.3"}).ToUserAgent()
	require.Equal(t, s, "some#funky#user#agent/#1.2.3")
}
