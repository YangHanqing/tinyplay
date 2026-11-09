package website

import (
	"os/exec"
	"testing"
)

func TestControllerBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the injected controller behavior tests")
	}
	cmd := exec.Command(node, "--test", "controller_behavior_test.cjs")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("controller behavior tests: %v\n%s", err, output)
	}
}
