##@ Generated Code/Files

.PHONY: generate
generate: gen-protos ## Generate all code, protos, etc.

.PHONY: gen-protos
gen-protos: buf ## Generate protobuf and gRPC code using buf.
	$(BUF) generate --template buf.gen.yaml
