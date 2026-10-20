package tools

import (
	"strings"
	"testing"
)

func TestMoreUtilities(t *testing.T) {
	s, _ := newShell(t)
	cases := []struct{ cmd, want string }{
		{`sed -n '2p' notes.txt`, "beta 1"},
		{`sed 's/alpha/ALPHA/' notes.txt | head -1`, "ALPHA 3"},
		{`seq 3 | xargs echo`, "1 2 3"},
		{`date -u -d '2026-01-02T03:04:05Z' +%Y-%m-%d`, "2026-01-02"},
	}
	for _, c := range cases {
		r := run(t, s, c.cmd)
		if !r.OK || strings.TrimSpace(r.Output) != c.want {
			t.Errorf("%s: got ok=%v %q, want %q", c.cmd, r.OK, r.Output, c.want)
		}
	}
}
