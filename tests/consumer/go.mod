module github.com/KLORQ/quarshell-sdk/tests/consumer

go 1.27.0

toolchain go1.27.3

require github.com/KLORQ/quarshell-sdk v0.0.0

require (
	github.com/KLORQ/quarshell-proto v0.2.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace github.com/KLORQ/quarshell-sdk => ../..
