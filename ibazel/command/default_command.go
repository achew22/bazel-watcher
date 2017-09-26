package command

import (
	"os/exec"
	"syscall"

	"github.com/bazelbuild/bazel-watcher/bazel"
)

type defaultCommand struct {
	target string
	b      bazel.Bazel
	args   []string
	cmd    *exec.Cmd
}

func DefaultCommand(bazel bazel.Bazel, target string, args []string) Command {
	return &defaultCommand{
		target: target,
		b:      bazel,
		args:   args,
	}
}

func (c *defaultCommand) Terminate() {
	if !subprocessRunning(c.cmd) {
		return
	}

	// Kill it with fire by sending SIGKILL to the process PID which should
	// propagate down to any subprocesses in the PGID (Process Group ID). To
	// send to the PGID, send the signal to the negative of the process PID.
	// Normally I would do this by calling c.cmd.Process.Signal, but that
	// only goes to the PID not the PGID.
	syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	c.cmd.Wait()
	c.cmd = nil
}

func (c *defaultCommand) Start() {
	c.cmd = run(c.b, c.target, c.args)
}

func (c *defaultCommand) NotifyOfChanges() {
	c.Terminate()
}

func (c *defaultCommand) IsSubprocessRunning() bool {
	return subprocessRunning(c.cmd)
}
