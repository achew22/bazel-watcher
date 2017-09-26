package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestIBazelRun_killPrexistiingJobWhenStarting(t *testing.T) {
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}

	defer i.Cleanup()

	// Create a process that has been started and can be killed
	toKill := exec.Command("sleep", "5s")
	toKill.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	toKill.Start()

	i.cmd = TestDefaultCommand(mockBazel, "//path/to:target", []string{}, toKill)

	cmd := exec.Command("ls")
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return cmd
	}
	i.cmd.Start()

	expected := [][]string{
		[]string{"Cancel"},
		[]string{"WriteToStderr"},
		[]string{"WriteToStdout"},
		[]string{"Run", "--script_path=.*", "//path/to:target"},
		[]string{"Run", "--script_path=.*", "//path/to:target"}, // TODO: investigate why this is being called twice
	}

	mockBazel.AssertActions(t, expected)

	if cmd.Stdout != os.Stdout {
		t.Errorf("Didn't set Stdout correctly")
	}
	if cmd.Stderr != os.Stderr {
		t.Errorf("Didn't set Stderr correctly")
	}
	if cmd.SysProcAttr.Setpgid != true {
		t.Errorf("Never set PGID (will prevent killing process trees -- see notes in ibazel.go")
	}

	if err := cmd.Wait(); err != nil {
		t.Errorf("New subprocess was never started. State: %v, Err: %v", cmd.ProcessState, err)
	}

	assertKilled(t, toKill)
}

// TODO: investigate why this test takes 200s to complete.
// func TestSubprocessRunning(t *testing.T) {
// 	i, err := New()
// 	if err != nil {
// 		t.Errorf("Error creating IBazel: %s", err)
// 	}
// 	defer i.Cleanup()

// 	cmd := exec.Command("sleep", "200ms")
// 	execCommand = func(name string, arg ...string) *exec.Cmd {
// 		return cmd
// 	}

// 	i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})

// 	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), false, "")
// 	i.cmd.Start()
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), true, "")
// 	cmd.Wait()
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), false, "")

// 	cmd = exec.Command("sleep", "1s")
// 	i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})

// 	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), false, "")
// 	i.cmd.Start()
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), true, "")

// 	i.cmd.Terminate()
// 	assertEqual(t, i.cmd.IsSubprocessRunning(), false, "")
// 	assertKilled(t, cmd)
// }

func TestKill(t *testing.T) {
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	defer i.Cleanup()

	cmd := exec.Command("sleep", "5s")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return cmd
	}
	i.cmd.Start()
	i.cmd.Terminate()
	assertKilled(t, cmd)
}

func TestHandleSignals_SIGINTWithoutRunningCommand(t *testing.T) {
	i := &IBazel{}
	err := i.setup()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	i.sigs = make(chan os.Signal, 1)
	defer i.Cleanup()

	// But we want to simulate the subprocess not dieing
	attemptedExit := 0
	osExit = func(i int) {
		attemptedExit = i
	}
	assertEqual(t, i.cmd, nil, "There shouldn't be a subprocess running")

	// SIGINT without a running command should attempt to exit
	i.sigs <- syscall.SIGINT
	i.handleSignals()

	// Goroutine tests are kind of racey
	assertEqual(t, attemptedExit, 3, "Should have exited ibazel")
}

func TestHandleSignals_SIGINT(t *testing.T) {
	i := &IBazel{}
	err := i.setup()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	i.sigs = make(chan os.Signal, 1)
	defer i.Cleanup()

	// But we want to simulate the subprocess not dieing
	attemptedExit := 0
	osExit = func(i int) {
		attemptedExit = i
	}

	var cmd *exec.Cmd
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return cmd
	}

	// Attempt to kill a task 2 times (but secretly resurrect the job from the
	// dead to test the job not responding)
	for j := 0; j < 2; j++ {
		// Start a task running for 5 seconds
		cmd = exec.Command("sleep", "5s")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})
		i.cmd.Start()

		// This should kill the subprocess and simulate hitting ctrl-c
		// First save the cmd so we can make assertions on it. It will be removed
		// by the SIGINT
		i.sigs <- syscall.SIGINT
		i.handleSignals()
		assertKilled(t, cmd)
		assertEqual(t, attemptedExit, 0, "It shouldn't have os.Exit'd")
	}

	cmd = exec.Command("sleep", "5s")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})
	i.cmd.Start()

	// This should kill the job and go over the interrupt limit where exiting happens
	i.sigs <- syscall.SIGINT
	i.handleSignals()
	assertKilled(t, cmd)

	assertEqual(t, attemptedExit, 3, "Should have exited ibazel")
}
func TestHandleSignals_SIGKILL(t *testing.T) {
	i := &IBazel{}
	err := i.setup()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	i.sigs = make(chan os.Signal, 1)
	defer i.Cleanup()

	// Now test sending SIGKILL
	attemptedExit := false
	osExit = func(i int) {
		attemptedExit = true
	}
	attemptedExit = false

	cmd := exec.Command("sleep", "1s")
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return cmd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	i.cmd = DefaultCommand(mockBazel, "//path/to:target", []string{})
	i.cmd.Start()

	i.sigs <- syscall.SIGKILL
	i.handleSignals()
	assertKilled(t, cmd)

	assertEqual(t, attemptedExit, true, "Should have exited ibazel")
}
