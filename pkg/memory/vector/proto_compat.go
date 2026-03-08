package vector

// This file documents the protobuf registration conflict between gRPC-based
// vector store clients (e.g. Qdrant, Milvus) and other dependencies.
//
// Some libraries register a proto file named "common.proto" with different
// Go packages and different message types. This triggers the default "panic"
// conflict policy in google.golang.org/protobuf/reflect/protoregistry.
//
// The conflict is benign — both proto files are completely unrelated. To
// suppress the panic, you MUST set the conflict policy to "warn" via one of:
//
//  1. Environment variable (recommended for running tests):
//     GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn go test ./...
//
//  2. Linker flag (recommended for building binaries):
//     go build -ldflags "-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn" ...
//
// See https://protobuf.dev/reference/go/faq#namespace-conflict
//
// The Makefile has been updated to include the appropriate flags.
