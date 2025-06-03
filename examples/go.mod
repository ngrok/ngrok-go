module golang.ngrok.com/ngrok/examples

go 1.21

require (
	github.com/valyala/fasthttp v1.56.0
	golang.ngrok.com/ngrok/v2 v2.0.0
)

require (
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.ngrok.com/muxado/v2 v2.0.1 // indirect
	golang.org/x/net v0.30.0 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
)

replace (
	golang.ngrok.com/ngrok/v2 => ../
	golang.ngrok.com/ngrok/v2/rpc => ../rpc
)
