package vector

// This file documents a historical protobuf registration conflict that existed
// when both Qdrant and Milvus vector store clients were used simultaneously.
// Both libraries registered a proto file named "common.proto" with different
// Go packages, triggering the default "panic" conflict policy in
// google.golang.org/protobuf/reflect/protoregistry.
//
// The Milvus backend was removed to eliminate this conflict. If a similar
// conflict arises from future dependencies, suppress the panic via one of:
//
//  1. Environment variable (recommended for running tests):
//     GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn go test ./...
//
//  2. Linker flag (recommended for building binaries):
//     go build -ldflags "-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn" ...
//
// See https://protobuf.dev/reference/go/faq#namespace-conflict
