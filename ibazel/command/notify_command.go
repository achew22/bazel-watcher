// Copyright 2017 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package command

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

type notifyCommand struct {
	target    string
	bazelArgs []string
	args      []string

	cmd   *exec.Cmd
	stdin io.WriteCloser
}

// NotifyCommand is an alternate mode for starting a command. In this mode the
// command will be notified on stdin that the source files have changed.
func NotifyCommand(bazelArgs []string, target string, args []string) Command {
	return &notifyCommand{
		target:    target,
		bazelArgs: bazelArgs,
		args:      args,
	}
}

func (c *notifyCommand) Terminate() {
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

func (c *notifyCommand) Start() {
	b := bazelNew()
	b.SetArguments(c.bazelArgs)

	b.WriteToStderr(true)
	b.WriteToStdout(true)

	c.cmd = start(b, c.target, c.args)

	var err error
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create OS pipe for stdin for subprocess. Err: %s", err)
	}
	// Keep the writer around.
	c.stdin = pw
	// And set the subprocess to take the reader.
	c.cmd.Stdin = pr

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting stdin pipe: %v\n", err)
	}

	c.cmd.Env = append(os.Environ(), "IBAZEL_NOTIFY_CHANGES=y")

	if err = c.cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting process: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "Starting...")
}

func (c *notifyCommand) NotifyOfChanges() {
	b := bazelNew()
	b.SetArguments(c.bazelArgs)

	b.WriteToStderr(true)
	b.WriteToStdout(true)

	err := b.Build(c.target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building target: %s\n%v", err)
	}

	res := b.Wait()
	if res != nil {
		fmt.Fprintf(os.Stderr, "FAILURE: %v\n", res)
		_, err := c.stdin.Write([]byte("IBAZEL_BUILD_COMPLETED FAILURE\n"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing failure to stdin: %s\n%v", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "SUCCESS\n")
		_, err := c.stdin.Write([]byte("IBAZEL_BUILD_COMPLETED SUCCESS\n"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing success to stdin: %s\n", err)
		}
	}
}

func (c *notifyCommand) IsSubprocessRunning() bool {
	return subprocessRunning(c.cmd)
}
