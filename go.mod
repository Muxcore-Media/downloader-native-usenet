module github.com/Muxcore-Media/downloader-native-usenet

go 1.26.4

require (
	github.com/Muxcore-Media/core/pkg/contracts v0.5.8
	github.com/Muxcore-Media/core/sdk/go/client v0.5.8
	github.com/Muxcore-Media/core/sdk/go/module v0.5.8
	github.com/go-newsgroups/nntp v0.1.0
	github.com/go-newsgroups/nzb v0.1.0
	github.com/go-newsgroups/yenc v0.1.0
	github.com/google/uuid v1.6.0
	github.com/hobeone/par2engine v1.0.6
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/Muxcore-Media/contracts-media v0.1.0 // indirect
	github.com/Muxcore-Media/core v0.5.8 // indirect
	github.com/Muxcore-Media/core/pkg/tenant v0.5.8 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260610212136-7ab31c22f7ad // indirect
)

replace github.com/Muxcore-Media/core => ../core

replace github.com/Muxcore-Media/core/pkg/contracts => ../core/pkg/contracts

replace github.com/Muxcore-Media/core/sdk/go/client => ../core/sdk/go/client

replace github.com/Muxcore-Media/core/sdk/go/module => ../core/sdk/go/module

replace github.com/Muxcore-Media/core/pkg/tenant => ../core/pkg/tenant
