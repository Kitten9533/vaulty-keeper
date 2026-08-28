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

func TestIsSensitiveKeyValueCredentialURI(t *testing.T) {
	// credential URIs must be masked
	cred := [][2]string{
		{"MONGODB_URI", "mongodb://root:pw@dds.example.com:3717/db?authSource=admin"},
		{"spring.datasource.url", "jdbc:mysql://user:pass@host:3306/db"},
		{"REDIS_URL", "redis://:secret@r-xxx.redis.rds.aliyuncs.com:6379"},
		{"DB_DSN", "postgres://app:secret@localhost:5432/app"},
		{"DATABASE_URL", "postgresql://u:p@host/db"},
		{"MONGODB_CONNECTION_STRING", "mongodb://u:p@host/db"},
		{"FTP_ENDPOINT", "ftp://user:pass@ftp.example.com"},
		{"RABBITMQ_ADDR", "amqp://guest:guest@localhost:5672/"},
	}
	for _, kv := range cred {
		if !IsSensitiveKeyValue(kv[0], kv[1]) {
			t.Errorf("%s = %s should be sensitive", kv[0], kv[1])
		}
	}
	// plain URIs without inline credentials stay visible
	plain := [][2]string{
		{"MONGODB_URI", "mongodb://dds.example.com:3717/db"},
		{"NEXT_PUBLIC_SUPABASE_URL", "https://awtlqcyqbeakebykugjk.supabase.co"},
		{"APP_URL", `"http://merdiexpress.com"`},
		{"CREATOR_MCP_BASE_URL", "https://test-imileexpress.52imile.cn"},
		{"REDIS_URI", "r-bp1vt3z0q58742w17y.redis.rds.aliyuncs.com"},
		{"API_ENDPOINT", "https://api.example.com/v1"},
		{"S3_ENDPOINT", "https://oss-cn-hangzhou.aliyuncs.com"},
		// "@" in a path (not userinfo) is not a credential
		{"BASE_URL", "https://example.com/@user/profile"},
	}
	for _, kv := range plain {
		if IsSensitiveKeyValue(kv[0], kv[1]) {
			t.Errorf("%s = %s should NOT be sensitive", kv[0], kv[1])
		}
	}
	// name-based sensitivity still applies regardless of value
	if !IsSensitiveKeyValue("DB_PASSWORD", "hunter2") {
		t.Error("DB_PASSWORD should be sensitive")
	}
}

func TestIsSensitiveKeyValueJWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImF3dGxxY3lxYmVha2VieWt1Z2prIiwicm9sZSI6ImFub24iLCJpYXQiOjE3NzQ2ODQ0MzEsImV4cCI6MjA5MDI2MDQzMX0.2kbuqpKCMUH_W5AOLLnk4wUrkWJVjOkYZkPiMwHv9uI"
	cases := [][2]string{
		{"SUPABASE_SERVICE_ROLE_KEY", jwt},
		{"NEXT_PUBLIC_SUPABASE_ANON_KEY", jwt},
		{"SOME_JWT", `"` + jwt + `"`}, // quoted value still detected
		{"ANY_KEY", jwt},
	}
	for _, kv := range cases {
		if !IsSensitiveKeyValue(kv[0], kv[1]) {
			t.Errorf("%s = %q should be sensitive (JWT)", kv[0], kv[1])
		}
	}
	plain := [][2]string{
		{"SUPABASE_URL", "https://awtlqcyqbeakebykugjk.supabase.co"},
		{"ANON_KEY", "eyJ-is-not-a-jwt-without-segments"},
		{"JWT_EXPIRES_IN", "3600"},
		{"BASE64", "eyJhbGciOiJIUzI1NiJ9eyJpc3MiOiJzdXBhYmFzZSJ9c2lnbmF0dXJl"},
		{"TWO_SEGMENT_VALUE", "abc.def"}, // two segments, not a JWT
	}
	for _, kv := range plain {
		if IsSensitiveKeyValue(kv[0], kv[1]) {
			t.Errorf("%s = %q should NOT be sensitive", kv[0], kv[1])
		}
	}
}
