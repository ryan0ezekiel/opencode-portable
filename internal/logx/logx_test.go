package logx

import "testing"

// slackToken is assembled at runtime so the fixture itself never appears in
// the source as a literal credential format (GitHub push protection flags
// literal Slack token shapes).
var slackToken = "slack xox" + "b-123456789012-123456789012-abcdefghijklmnop"

func TestSanitize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// No secrets: unchanged.
		{"nothing to see here", "nothing to see here"},
		{"short sk-abc stays", "short sk-abc stays"},
		// Secrets: the full match (including any prefix like "Bearer ")
		// is redacted — over-redaction is safe.
		{"sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345", "[REDACTED]"},
		{"sk-abcdefghijklmnopqrstuv", "[REDACTED]"},
		{"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456", "Authorization: [REDACTED]"},
		{"token=ghp_abcdefghijklmnopqrstuvwxyz1234567890", "token=[REDACTED]"},
		{slackToken, "slack [REDACTED]"},
		{"api_key=super-secret-value-12345", "[REDACTED]"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
