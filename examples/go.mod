module golang.ngrok.com/ngrok/examples

go 1.20

require (
	golang.ngrok.com/ngrok v0.0.0
	golang.ngrok.com/ngrok/log/slog v0.0.0-00010101000000-000000000000
	golang.org/x/exp v0.0.0-20230307190834-24139beb5833
)

require (
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/inconshreveable/log15 v3.0.0-testing.3+incompatible // indirect
	github.com/inconshreveable/log15/v3 v3.0.0-testing.5 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.ngrok.com/muxado/v2 v2.0.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/term v0.8.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
)

replace (
	golang.ngrok.com/ngrok => ../
	golang.ngrok.com/ngrok/log/log15 => ../log/log15
	golang.ngrok.com/ngrok/log/slog => ../log/slog
)
