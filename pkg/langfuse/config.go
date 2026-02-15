package langfuse

import (
	"os"
	"strconv"
	"strings"

	"github.com/appcd-dev/go-lib/osutils"
)

var (
	LangfusePublicKey = osutils.Getenv("LANGFUSE_PUBLIC_KEY", "")
	LangfuseSecretKey = osutils.Getenv("LANGFUSE_SECRET_KEY", "")
	LangfuseHost      = osutils.Getenv("LANGFUSE_HOST", "langfuse.cloud.stackgen.com")
	EnablePrompts     = getBoolEnv("LANGFUSE_ENABLE_PROMPTS", false)
)

func langfuseHost() string {
	if strings.HasPrefix(LangfuseHost, "https://") || strings.HasPrefix(LangfuseHost, "http://") {
		return LangfuseHost
	}
	return "https://" + LangfuseHost
}

func getBoolEnv(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}
