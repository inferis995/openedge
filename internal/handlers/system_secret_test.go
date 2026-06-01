package handlers

import "testing"

// isSecretKey is the keyword-based gate that both GetSettings (mask on
// read) and applyPrefixedSettings (preserve-on-empty-write) consult.
// Keep these in sync so a new credential-like field doesn't accidentally
// leak through one and stay editable through the other.
func TestIsSecretKey(t *testing.T) {
	secret := []string{
		"notif_email_password",
		"notif_telegram_bot_token",
		"some_api_key",
		"private_signing_key",
		"db_password",
		"mqtt_password",
	}
	for _, k := range secret {
		if !isSecretKey(k) {
			t.Errorf("isSecretKey(%q) = false, want true", k)
		}
	}

	notSecret := []string{
		"publish_mode",
		"notif_email_enabled",
		"notif_email_smtp_host",
		"notif_email_smtp_port",
		"notif_email_username",
		"notif_email_to",
		"notif_min_severity",
		"backup_enabled",
		"backup_schedule",
		"backup_retention_days",
		"backup_age_recipient", // age PUBLIC key — not a secret, displayed in UI
		"deployment_mode",
		"db_retention_days",
	}
	for _, k := range notSecret {
		if isSecretKey(k) {
			t.Errorf("isSecretKey(%q) = true, want false", k)
		}
	}
}
