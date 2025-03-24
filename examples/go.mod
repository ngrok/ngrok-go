module golang.ngrok.com/ngrok/examples

go 1.21

require (
	github.com/valyala/fasthttp v1.56.0
	golang.ngrok.com/ngrok v1.11.0
	golang.ngrok.com/ngrok/log/slog v0.0.0-20241014162652-57e91a614efd
)

require (
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/inconshreveable/log15 v3.0.0-testing.5+incompatible // indirect
	github.com/inconshreveable/log15/v3 v3.0.0-testing.5 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.ngrok.com/muxado/v2 v2.0.1 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/term v0.25.0 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	golang.ngrok.com/ngrok => ../
	golang.ngrok.com/ngrok/log/log15 => ../log/log15
	golang.ngrok.com/ngrok/log/slog => ../log/slog
)
