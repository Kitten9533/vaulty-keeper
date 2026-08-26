package apollo

import "testing"

func TestIsSensitive(t *testing.T) {
	sensitive := []string{
		"PASSWORD", "password", "DB_PASSWORD", "PASSWORD_SALT",
		"TOKEN", "ACCESS_TOKEN", "ACCESS_TOKEN_EXPIRES",
		"SECRET", "SECRET_KEY", "secret-key", "SECRET_KEY_ID",
		"imile.fs.oss.secret-key", "imile.fs.oss.access-key-id",
		"CREDENTIAL", "PRIVATE_KEY", "API_KEY", "APP_SECRET",
		"ACCESS_KEY_ID",
	}
	plain := []string{
		"APP_NAME", "REDIS_PORT", "COOKIE_MAX_AGE", "CMS_JWT_EXPIRES_IN",
		"MONGODB_NAME", "REDIS_URI", "URL", "HOST", "TIMEOUT", "MAX_RETRY",
		"ENABLED_ENCRYPT", "ENDPOINT", "BUCKET_NAME",
	}
	for _, k := range sensitive {
		if !IsSensitive(k) {
			t.Errorf("%q should be sensitive", k)
		}
	}
	for _, k := range plain {
		if IsSensitive(k) {
			t.Errorf("%q should not be sensitive", k)
		}
	}
}
