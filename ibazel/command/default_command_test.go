package command

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	mock_bazel "github.com/bazelbuild/bazel-watcher/bazel/testing"
)

func TestDefaultCommand_Start(t *testing.T) {
	b := &mock_bazel.MockBazel{}
	c := &defaultCommand{
		args:   []string{"moo"},
		b:      b,
		target: "//path/to:target",
	}

	c.Start() // This is supposed to fail because //path/to:target won't exist.

	if c.cmd.Stdout != os.Stdout {
		t.Errorf("Didn't set Stdout correctly")
	}
	if c.cmd.Stderr != os.Stderr {
		t.Errorf("Didn't set Stderr correctly")
	}
	if c.cmd.SysProcAttr.Setpgid != true {
		t.Errorf("Never set PGID (will prevent killing process trees -- see notes in ibazel.go")
	}

	b.AssertActions(t, [][]string{
		[]string{"Run", "--script_path=.*", "//path/to:target"},
	})
}

func TestDefaultCommand(t *testing.T) {
	toKill := exec.Command("sleep", "5s")
	toKill.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	b := &mock_bazel.MockBazel{}
	c := &defaultCommand{
		args:   []string{"moo"},
		b:      b,
		cmd:    toKill,
		target: "//path/to:target",
	}

	if c.IsSubprocessRunning() {
		t.Errorf("New subprocess shouldn't have been started yet. State: %v", toKill.ProcessState)
	}

	toKill.Start()

	if !c.IsSubprocessRunning() {
		t.Errorf("New subprocess was never started. State: %v", toKill.ProcessState)
	}

	// This is synonamous with killing the job so use it to kill the job and test everything.
	c.NotifyOfChanges()
	assertKilled(t, toKill)
}
