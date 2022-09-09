module github.com/ngrok/libngrok-go/log/log15adapter

go 1.18

require (
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac
	github.com/ngrok/libngrok-go/log v0.0.0
)

require (
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	golang.org/x/sys v0.0.0-20220728004956-3c1f35247d10 // indirect
)

replace github.com/ngrok/libngrok-go/log => ../
