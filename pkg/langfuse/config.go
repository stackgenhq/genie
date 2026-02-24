package langfuse

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/httputil"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/ttlcache"
)

// Config holds the configuration for the Langfuse integration, which
// provides observability and tracing for LLM interactions.
type Config struct {
	PublicKey     string `json:"public_key" toml:"public_key" yaml:"public_key"`
	SecretKey     string `json:"secret_key" toml:"secret_key" yaml:"secret_key"`
	Host          string `json:"host" toml:"host" yaml:"host"`
	EnablePrompts bool   `json:"enable_prompts" toml:"enable_prompts" yaml:"enable_prompts"`
}

// DefaultConfig builds the default Langfuse configuration by resolving
// credentials through the given SecretProvider. Without a SecretProvider,
// callers can pass security.NewEnvProvider() to preserve the legacy
// os.Getenv behavior.
func DefaultConfig(ctx context.Context, sp security.SecretProvider) Config {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, name)
		return v
	}

	return Config{
		PublicKey:     get("LANGFUSE_PUBLIC_KEY"),
		SecretKey:     get("LANGFUSE_SECRET_KEY"),
		Host:          get("LANGFUSE_HOST"),
		EnablePrompts: os.Getenv("LANGFUSE_ENABLE_PROMPTS") == "true",
	}
}

// langfuseHost returns the full URL (with scheme) for the Langfuse HTTP API.
func (c Config) langfuseHost() string {
	if strings.HasPrefix(c.Host, "https://") || strings.HasPrefix(c.Host, "http://") {
		return c.Host
	}
	return "https://" + c.Host
}

// langfuseOTLPEndpoint returns the host in "hostname:port" format for the OTLP exporter.
func (c Config) langfuseOTLPEndpoint() string {
	host := c.Host
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}
	return host
}

func (c Config) NewClient() Client {
	if c.PublicKey == "" || c.SecretKey == "" || c.Host == "" {
		return &noopClient{}
	}
	langfuseClient := &client{
		httpClient: httputil.GetClient(func(req *http.Request) {
			req.SetBasicAuth(c.PublicKey, c.SecretKey)
		}),
	}
	langfuseClient.promptsCache = ttlcache.NewItem(langfuseClient.getAllPrompts, 10*time.Minute)
	return langfuseClient
}
