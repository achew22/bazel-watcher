package command

import (
	"testing"
	// "github.com/bazelbuild/bazel-watcher/ibazel"
	"os/exec"
)

func assertKilled(t *testing.T, cmd *exec.Cmd) {
	if err := cmd.Wait(); err != nil {
		if cmd.ProcessState.Success() {
			t.Errorf("Subprocess terminated from \"natural\" causes, which means the job ran till its timeout then existed. The Run method should have killed it before then.")
		}
		if cmd.ProcessState == nil {
			t.Errorf("Killable subprocess was never started. State: %v, Err: %v", cmd.ProcessState, err)
		}
	}
}

func TestSubprocessRunning(t *testing.T) {
	if subprocessRunning(nil) {
		t.Errorf("Nil subprocesses don't run")
	}

	cmd := exec.Command("sleep", ".1s")

	if subprocessRunning(cmd) {
		t.Errorf("New subprocess shouldn't have been started yet. State: %v", cmd.ProcessState)
	}

	cmd.Start()

	if !subprocessRunning(cmd) {
		t.Errorf("New subprocess was never started. State: %v", cmd.ProcessState)
	}

	err := cmd.Wait()
	if err != nil || subprocessRunning(cmd) {
		t.Errorf("Subprocess finished with error: %v State: %v", err, cmd.ProcessState)
	}
}
