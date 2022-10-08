module github.com/ngrok/ngrok-go/examples

go 1.18

require (
	github.com/ngrok/ngrok-go v0.0.0
	golang.org/x/sync v0.0.0-20220923202941-7f9b1623fab7
)

require (
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	golang.org/x/net v0.0.0-20220812174116-3211cb980234 // indirect
	golang.org/x/sys v0.0.0-20220728004956-3c1f35247d10 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace (
	github.com/ngrok/ngrok-go => ../
	github.com/ngrok/ngrok-go/log/log15adapter => ../log/log15adapter
)
