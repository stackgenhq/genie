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
//	[messenger.agui.auth.oauth]
//	client_id = "..."
//	client_secret = "..."
//	allowed_domains = ["stackgen.com"]
type Config struct {
	Password PasswordConfig `yaml:"password,omitempty" toml:"password,omitempty"`
	JWT      JWTConfig      `yaml:"jwt,omitempty" toml:"jwt,omitempty"`
	OAuth    OAuthConfig    `yaml:"oauth,omitempty" toml:"oauth,omitempty"`
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

// JWTConfig configures JWT/OIDC token validation for API-level authentication.
// Uses go-oidc for full cryptographic signature verification via JWKS auto-discovery.
type JWTConfig struct {
	// TrustedIssuers is a list of OIDC issuer URLs whose JWTs are accepted.
	// Each issuer must serve a standard .well-known/openid-configuration.
	//
	// Examples:
	//   - "https://accounts.google.com"
	//   - "https://login.microsoftonline.com/{tenant-id}/v2.0"
	//   - "https://dev-12345.okta.com/oauth2/default"
	//   - "https://cognito-idp.{region}.amazonaws.com/{user-pool-id}"
	TrustedIssuers []string `yaml:"trusted_issuers,omitempty" toml:"trusted_issuers,omitempty"`

	// AllowedAudiences is an optional list of expected "aud" claim values.
	// When non-empty, JWT tokens must have an audience matching at least one
	// entry. When empty, any audience from a trusted issuer is accepted.
	AllowedAudiences []string `yaml:"allowed_audiences,omitempty" toml:"allowed_audiences,omitempty"`
}

// Enabled returns true when JWT validation is configured.
func (j JWTConfig) Enabled() bool {
	return len(j.TrustedIssuers) > 0
}

// OAuthConfig configures the Google OAuth 2.0 / OIDC browser login flow.
// When both ClientID and ClientSecret are set, the server exposes
// /auth/login, /auth/callback, and /auth/logout endpoints.
type OAuthConfig struct {
	// ClientID is the Google OAuth 2.0 Client ID from the Cloud Console.
	ClientID string `yaml:"client_id,omitempty" toml:"client_id,omitempty"`

	// ClientSecret is the Google OAuth 2.0 Client Secret.
	ClientSecret string `yaml:"client_secret,omitempty" toml:"client_secret,omitempty"`

	// AllowedDomains restricts OAuth login to users from these Google Workspace
	// domains. For example, ["stackgen.com"] only allows @stackgen.com accounts.
	// When empty, any Google account is allowed.
	AllowedDomains []string `yaml:"allowed_domains,omitempty" toml:"allowed_domains,omitempty"`

	// CookieSecret is a 32+ byte key used to HMAC-sign session cookies.
	// If empty, a random key is generated at startup (sessions won't survive
	// server restarts). Can be set via AGUI_COOKIE_SECRET env var.
	CookieSecret string `yaml:"cookie_secret,omitempty" toml:"cookie_secret,omitempty"`

	// RedirectURL is the full URL of /auth/callback as registered in Google
	// Cloud Console. Example: "https://genie.example.com/auth/callback".
	// If empty, it's auto-detected from the incoming request Host header.
	RedirectURL string `yaml:"redirect_url,omitempty" toml:"redirect_url,omitempty"`
}

// Enabled returns true when the OAuth login flow is configured.
func (o OAuthConfig) Enabled() bool {
	return o.ClientID != "" && o.ClientSecret != ""
}
