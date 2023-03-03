package proto

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObfuscatedString(t *testing.T) {
	t.Parallel()

	fakeToken := "weeeeee"
	obfuscatedString := ObfuscatedString(fakeToken)
	require.Equal(t, fakeToken, obfuscatedString.PlainText())

	// Intentionally doing something a little funky here to make
	// sure that the String() method is doing what it's supposed to.
	//nolint:gosimple
	printedString := fmt.Sprintf("%s", obfuscatedString)
	require.NotEqual(t, fakeToken, printedString)
	require.Equal(t, "HIDDEN", printedString)
}
