module golang.ngrok.com/ngrok/examples

go 1.18

require (
	golang.ngrok.com/ngrok v0.0.0
	golang.org/x/sync v0.0.0-20220923202941-7f9b1623fab7
)

require (
	github.com/inconshreveable/log15/v3 v3.0.0-testing.1 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	golang.org/x/net v0.2.0 // indirect
	golang.org/x/sys v0.2.0 // indirect
	golang.org/x/term v0.2.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace (
	golang.ngrok.com/ngrok => ../
	golang.ngrok.com/ngrok/log/log15adapter => ../log/log15adapter
)
