package main

import (
	"io"
	"strings"
	"testing"
)

func TestBadIntervalFailsAtStartup(t *testing.T) {
	for _, arg := range []string{"5s", "9.9s", "-1m", "soon"} {
		err := run([]string{"214", "--interval", arg}, func(string) string { return "" }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "interval") {
			t.Errorf("--interval %s: err = %v, want one naming the flag", arg, err)
		}
	}
}
