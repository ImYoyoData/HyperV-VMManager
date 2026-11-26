package credentials

import (
	"os"
	"testing"
)

func TestManualLoad(t *testing.T) {
	host := os.Getenv("HV_CRED_HOST")
	if host == "" {
		t.Skip("未设置 HV_CRED_HOST")
	}
	user, pass, ok := Load(host)
	t.Logf("ok=%v user=%q passLen=%d", ok, user, len(pass))
}
