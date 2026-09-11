package invest

import (
	"context"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("INVEST_TOKEN", "test-token")
	t.Setenv("INVEST_ENV", "")
	t.Setenv("INVEST_APP_NAME", "")
	t.Setenv("INVEST_TIMEOUT", "")
	c, err := ConfigFromEnv()
	if err != nil || c.Environment != "sandbox" || c.Timeout != 10*time.Second {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"INVEST_TOKEN", ""}, {"INVEST_ENV", "prod"}, {"INVEST_TIMEOUT", "0s"}, {"INVEST_TIMEOUT", "-1s"}, {"INVEST_TIMEOUT", "bad"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
func TestAuthRequiresTLS(t *testing.T) {
	a := auth{token: "test-token", appName: "test-app"}
	md, err := a.GetRequestMetadata(context.Background())
	if err != nil || md["authorization"] != "Bearer test-token" || md["x-app-name"] != "test-app" || !a.RequireTransportSecurity() {
		t.Fatal("invalid credentials")
	}
}
