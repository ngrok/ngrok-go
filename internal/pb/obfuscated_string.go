package pb

type ObfuscatedString string

func (t ObfuscatedString) String() string {
	return "HIDDEN"
}

func (t ObfuscatedString) PlainText() string {
	return string(t)
}
