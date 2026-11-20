package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoginShellWithoutSHELL(t *testing.T) {
	passwd := filepath.Join(t.TempDir(), "passwd")
	os.WriteFile(passwd, []byte("root:x:0:0::/root:/bin/bash\nada:x:1000:1000::/home/ada:/usr/bin/fish\n"), 0o644)
	for _, tc := range []struct {
		env, goos string
		uid       int
		want      string
	}{
		{"/bin/ksh", "linux", 1000, "/bin/ksh"},
		{"", "linux", 1000, "/usr/bin/fish"},
		{"", "linux", 42, "/bin/sh"},
	} {
		if got := loginShell(tc.env, tc.goos, passwd, tc.uid); got != tc.want {
			t.Errorf("%+v: got %s", tc, got)
		}
	}
}
