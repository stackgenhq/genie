package notification

type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url" toml:"webhook_url"`
}

type WebhookConfig struct {
	URL     string            `yaml:"url" toml:"url"`
	Headers map[string]string `yaml:"headers,omitempty" toml:"headers,omitempty"`
}

type TwilioConfig struct {
	AccountSID string `yaml:"account_sid" toml:"account_sid"`
	AuthToken  string `yaml:"auth_token" toml:"auth_token"`
	From       string `yaml:"from" toml:"from"`
	To         string `yaml:"to" toml:"to"`
}

type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url" toml:"webhook_url"`
}

type Config struct {
	Slack    []SlackConfig   `yaml:"slack,omitempty" toml:"slack,omitempty"`
	Webhooks []WebhookConfig `yaml:"webhooks,omitempty" toml:"webhooks,omitempty"`
	Twilio   []TwilioConfig  `yaml:"twilio,omitempty" toml:"twilio,omitempty"`
	Discord  []DiscordConfig `yaml:"discord,omitempty" toml:"discord,omitempty"`
}

func (c Config) IsEmpty() bool {
	return len(c.Slack) == 0 && len(c.Webhooks) == 0 && len(c.Twilio) == 0 && len(c.Discord) == 0
}
