package langfuse

import (
	"github.com/appcd-dev/go-lib/osutils"
)

var (
	LangfusePublicKey = osutils.Getenv("LANGFUSE_PUBLIC_KEY", "")
	LangfuseSecretKey = osutils.Getenv("LANGFUSE_SECRET_KEY", "")
	LangfuseHost      = osutils.Getenv("LANGFUSE_HOST", "langfuse.cloud.stackgen.com")
)
