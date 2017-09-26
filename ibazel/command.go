package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"

	"github.com/bazelbuild/bazel-watcher/bazel"
)

var execCommand = exec.Command

type Command interface {
	Start()
	Terminate()
	NotifyOfChanges()
	IsSubprocessRunning() bool
}

func run(b bazel.Bazel, target string, args []string) *exec.Cmd {
	tmpfile, err := ioutil.TempFile("", "bazel_script_path")
	if err != nil {
		fmt.Print(err)
	}
	// Close the file so bazel can write over it
	if err := tmpfile.Close(); err != nil {
		fmt.Print(err)
	}

	// Start by building the binary
	b.Run("--script_path="+tmpfile.Name(), target)

	targetPath := tmpfile.Name()

	// Now that we have built the target, construct a executable form of it for
	// execution in a go routine.
	cmd := execCommand(targetPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set a process group id (PGID) on the subprocess. This is
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start run in a goroutine so that it doesn't block watching for files that
	// have changed.
	if err := cmd.Start(); err != nil {
		fmt.Printf("Error starting process: %v\n", err)
	}

	return cmd
}

func subprocessRunning(cmd *exec.Cmd) bool {
	if cmd == nil {
		return false
	}
	if cmd.Process == nil {
		return false
	}
	if cmd.ProcessState != nil {
		if cmd.ProcessState.Exited() {
			return false
		}
	}

	return true
}
