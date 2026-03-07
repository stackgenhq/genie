package auth

// Config holds all authentication settings for the AG-UI server.
// Each auth method is isolated into its own sub-config for clarity.
//
// TOML layout:
//
//	[messenger.agui.auth.password]
//	enabled = true
//
//	[messenger.agui.auth.jwt]
//	trusted_issuers = ["https://accounts.google.com"]
//
//	[messenger.agui.auth.api_keys]
//	keys = ["secret-1", "secret-2"]
//
//	[messenger.agui.auth.oidc]
//	issuer_url = "https://accounts.google.com"
//	client_id = "..."
//	client_secret = "..."
//	allowed_domains = ["stackgen.com"]
type Config struct {
	Password PasswordConfig `yaml:"password,omitempty" toml:"password,omitempty"`
	JWT      JWTConfig      `yaml:"jwt,omitempty" toml:"jwt,omitempty"`
	APIKeys  APIKeyConfig   `yaml:"api_keys,omitempty" toml:"api_keys,omitempty"`
	OIDC     OIDCConfig     `yaml:"oidc,omitempty" toml:"oidc,omitempty"`
}

// PasswordConfig configures password-based authentication via the
// X-AGUI-Password header or ?password= query param.
type PasswordConfig struct {
	// Enabled turns on password protection. The password is resolved in order:
	//   1. Value field (below)
	//   2. AGUI_PASSWORD environment variable
	//   3. OS keyring (for local/desktop use)
	//   4. Auto-generated random password (logged at startup)
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`

	// Value is the plaintext shared secret. Prefer AGUI_PASSWORD env var
	// for cloud/container deployments where keyring is unavailable.
	Value string `yaml:"value,omitempty" toml:"value,omitempty"`
}

// JWTConfig configures JWT token validation for API-level authentication.
// Uses go-oidc for full cryptographic signature verification via JWKS auto-discovery.
type JWTConfig struct {
	// TrustedIssuers is a list of OIDC issuer URLs whose JWTs are accepted.
	TrustedIssuers []string `yaml:"trusted_issuers,omitempty" toml:"trusted_issuers,omitempty"`

	// AllowedAudiences is an optional list of expected "aud" claim values.
	// When non-empty, JWT tokens must have an audience matching at least one entry.
	AllowedAudiences []string `yaml:"allowed_audiences,omitempty" toml:"allowed_audiences,omitempty"`
}

// Enabled returns true when JWT validation is configured.
func (j JWTConfig) Enabled() bool {
	return len(j.TrustedIssuers) > 0
}

// APIKeyConfig configures static API keys for machine-to-machine authentication.
type APIKeyConfig struct {
	// Keys is a list of static secrets accepted via the Authorization: Bearer <token>
	// header or X-API-Key header.
	Keys []string `yaml:"keys,omitempty" toml:"keys,omitempty"`
}

// Enabled returns true when static API keys are configured.
func (a APIKeyConfig) Enabled() bool {
	return len(a.Keys) > 0
}

// OIDCConfig configures the generic OIDC browser login flow.
// When IssuerURL, ClientID, and ClientSecret are set, the server exposes
// /auth/login, /auth/callback, and /auth/logout endpoints.
type OIDCConfig struct {
	// IssuerURL is the OIDC provider's discovery URL (e.g. "https://accounts.google.com",
	// "https://your-tenant.okta.com", "https://dev-xxx.auth0.com").
	IssuerURL string `yaml:"issuer_url,omitempty" toml:"issuer_url,omitempty"`

	// ClientID is the OAuth 2.0 Client ID.
	ClientID string `yaml:"client_id,omitempty" toml:"client_id,omitempty"`

	// ClientSecret is the OAuth 2.0 Client Secret.
	ClientSecret string `yaml:"client_secret,omitempty" toml:"client_secret,omitempty"`

	// AllowedDomains restricts login to users from these domains (if supported
	// by the provider via the "hd" parameter, like Google Workspace).
	// When empty, any account from the provider is allowed.
	AllowedDomains []string `yaml:"allowed_domains,omitempty" toml:"allowed_domains,omitempty"`

	// CookieSecret is a 32+ byte key used to HMAC-sign session cookies.
	// If empty, a random key is generated at startup.
	CookieSecret string `yaml:"cookie_secret,omitempty" toml:"cookie_secret,omitempty"`

	// RedirectURL is the full URL of /auth/callback registered in the provider.
	// If empty, it's auto-detected from the incoming request Host header.
	RedirectURL string `yaml:"redirect_url,omitempty" toml:"redirect_url,omitempty"`
}

// Enabled returns true when the OIDC login flow is configured.
func (o OIDCConfig) Enabled() bool {
	return o.IssuerURL != "" && o.ClientID != "" && o.ClientSecret != ""
}
