package auth

// Config holds authentication settings for the AG-UI server.
// Embed this in the messenger AGUIConfig or pass it directly to Middleware().
type Config struct {
	// PasswordProtected enables password-based authentication via the
	// X-AGUI-Password header. The password is resolved in order:
	//   1. Password field (below)
	//   2. AGUI_PASSWORD environment variable
	//   3. OS keyring (for local/desktop use)
	//   4. Auto-generated random password (logged at startup)
	PasswordProtected bool `yaml:"password_protected,omitempty" toml:"password_protected,omitempty"`

	// Password is the plaintext shared secret for password auth.
	// Preferred for cloud/container deployments where keyring is unavailable.
	// Can also be set via the AGUI_PASSWORD environment variable.
	Password string `yaml:"password,omitempty" toml:"password,omitempty"`

	// TrustedIssuers is a list of OIDC issuer URLs whose JWTs are accepted.
	// Each issuer must serve a standard .well-known/openid-configuration
	// endpoint. When non-empty, JWT authentication is enabled and checked
	// before password auth.
	//
	// Examples:
	//   - "https://accounts.google.com"
	//   - "https://login.microsoftonline.com/{tenant-id}/v2.0"
	//   - "https://dev-12345.okta.com/oauth2/default"
	//   - "https://cognito-idp.{region}.amazonaws.com/{user-pool-id}"
	TrustedIssuers []string `yaml:"trusted_issuers,omitempty" toml:"trusted_issuers,omitempty"`

	// AllowedAudiences is an optional list of expected "aud" claim values.
	// When non-empty, JWT tokens must have an audience matching at least one
	// entry. When empty, audience is not checked (any audience from a
	// trusted issuer is accepted).
	AllowedAudiences []string `yaml:"allowed_audiences,omitempty" toml:"allowed_audiences,omitempty"`
}
