package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestMainExec(t *testing.T) {
	if os.Getenv("BE_KTPL_MAIN") == "1" {
		os.Args = []string{"ktpl", "--help"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainExec")
	cmd.Env = append(os.Environ(), "BE_KTPL_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process failed: %v\nOutput: %s", err, string(out))
	}
}
