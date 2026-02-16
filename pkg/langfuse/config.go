package langfuse

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/httputil"
	"github.com/appcd-dev/genie/pkg/ttlcache"
)

type Config struct {
	PublicKey     string `json:"public_key" toml:"public_key" yaml:"public_key"`
	SecretKey     string `json:"secret_key" toml:"secret_key" yaml:"secret_key"`
	Host          string `json:"host" toml:"host" yaml:"host"`
	EnablePrompts bool   `json:"enable_prompts" toml:"enable_prompts" yaml:"enable_prompts"`
}

func DefaultConfig() Config {
	return Config{
		PublicKey:     os.Getenv("LANGFUSE_PUBLIC_KEY"),
		SecretKey:     os.Getenv("LANGFUSE_SECRET_KEY"),
		Host:          os.Getenv("LANGFUSE_HOST"),
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
