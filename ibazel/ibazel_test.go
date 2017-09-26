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
	"reflect"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/bazelbuild/bazel-watcher/bazel"
	mock_bazel "github.com/bazelbuild/bazel-watcher/bazel/testing"
	"github.com/bazelbuild/bazel-watcher/ibazel/command"
	"github.com/fsnotify/fsnotify"
)

func assertEqual(t *testing.T, want, got interface{}, msg string) {
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Wanted %s, got %s. %s", want, got, msg)
		debug.PrintStack()
	}
}

type mockCommand struct {
	b                 bazel.Bazel
	target            string
	args              []string
	notifiedOfChanges bool
}

func (m *mockCommand) Start()     {}
func (m *mockCommand) Terminate() {}
func (m *mockCommand) NotifyOfChanges() {
	m.notifiedOfChanges = true
}
func (m *mockCommand) IsSubprocessRunning() bool {
	return false
}

var mockBazel *mock_bazel.MockBazel

func init() {
	// Replace the bazle object creation function with one that makes my mock.
	bazelNew = func() bazel.Bazel {
		mockBazel = &mock_bazel.MockBazel{}
		return mockBazel
	}
}

func TestIBazelLifecycle(t *testing.T) {
	t.Skip()
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}

	i.Cleanup()

	// Now inspect private API. If things weren't closed properly this will block
	// and the test will timeout.
	<-i.sourceFileWatcher.Events
	<-i.buildFileWatcher.Events
}

func TestIBazelLoop(t *testing.T) {
	t.Skip()
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}

	// Replace the file watching channel with one that has a buffer.
	i.buildFileWatcher.Events = make(chan fsnotify.Event, 1)
	i.sourceEventHandler.SourceFileEvents = make(chan fsnotify.Event, 1)

	defer i.Cleanup()

	// The process for testing this is going to be to emit events to the channels
	// that are associated with these objects and walk the state transition
	// graph.

	// First let's consume all the events from all the channels we care about
	called := false
	command := func(targets ...string) {
		called = true
	}

	i.state = QUERY
	step := func() {
		i.iteration("demo", command, []string{}, "")
	}
	assertRun := func() {
		if called == false {
			_, file, line, _ := runtime.Caller(1) // decorate + log + public function.
			t.Errorf("%s:%v Should have run the provided comand", file, line)
		}
		called = false
	}
	assertState := func(state State) {
		if i.state != state {
			_, file, line, _ := runtime.Caller(1) // decorate + log + public function.
			t.Errorf("%s:%v Expected state to be %s but was %s", file, line, state, i.state)
		}
	}

	// Pretend a fairly normal event chain happens.
	// Start, run the program, write a source file, run, write a build file, run.

	assertState(QUERY)
	step()
	assertState(RUN)
	step() // Actually run the command
	assertRun()
	assertState(WAIT)
	// Source file change.
	i.sourceEventHandler.SourceFileEvents <- fsnotify.Event{}
	step()
	assertState(DEBOUNCE_RUN)
	step()
	// Don't send another event in to test the timer
	assertState(RUN)
	step() // Actually run the command
	assertRun()
	assertState(WAIT)
	// Build file change.
	i.buildFileWatcher.Events <- fsnotify.Event{}
	step()
	assertState(DEBOUNCE_QUERY)
	// Don't send another event in to test the timer
	step()
	assertState(QUERY)
	step()
	assertState(RUN)
	step() // Actually run the command
	assertRun()
	assertState(WAIT)
}

func TestIBazelBuild(t *testing.T) {
	t.Skip()
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}

	defer i.Cleanup()

	i.build("//path/to:target")
	expected := [][]string{
		[]string{"Cancel"},
		[]string{"WriteToStderr"},
		[]string{"WriteToStdout"},
		[]string{"Build", "//path/to:target"},
	}

	mockBazel.AssertActions(t, expected)
}

func TestIBazelTest(t *testing.T) {
	t.Skip()
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}

	defer i.Cleanup()

	i.test("//path/to:target")
	expected := [][]string{
		[]string{"Cancel"},
		[]string{"WriteToStderr"},
		[]string{"WriteToStdout"},
		[]string{"Test", "//path/to:target"},
	}

	mockBazel.AssertActions(t, expected)
}

func TestIBazelRun_firstPass(t *testing.T) {
	t.Skip()
	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	defer i.Cleanup()

	i.run("//path/to:target")

	expected := [][]string{
		[]string{"Cancel"},
		[]string{"WriteToStderr"},
		[]string{"WriteToStdout"},
		[]string{"Run", "--script_path=.*", "//path/to:target"},
	}

	mockBazel.AssertActions(t, expected)
}

func TestIBazelRun_notifyPrexistiingJobWhenStarting(t *testing.T) {
	oldDefaultCommand := commandDefaultCommand
	commandDefaultCommand = func(b bazel.Bazel, target string, args []string) command.Command {
		// Don't do anything
		return &mockCommand{
			b:      b,
			target: target,
			args:   args,
		}
	}
	defer func() { commandDefaultCommand = oldDefaultCommand }()

	i, err := New()
	if err != nil {
		t.Errorf("Error creating IBazel: %s", err)
	}
	defer i.Cleanup()

	i.args = []string{"--do_it"}

	cmd := &mockCommand{
		notifiedOfChanges: false,
	}
	i.cmd = cmd

	path := "//path/to:target"
	i.run(path)

	if !cmd.notifiedOfChanges {
		t.Errorf("The preiously running command was not notified of changes")
	}

	expected := [][]string{
		[]string{"Cancel"},
		[]string{"WriteToStderr"},
		[]string{"WriteToStdout"},
		// it's last action was to call cmd.start, but that was mocked out,
		// so we can just inspect the mock and see what it did.
	}
	mockBazel.AssertActions(t, expected)

	c, ok := i.cmd.(*mockCommand)
	if !ok {
		t.Errorf("Unable to cast i.cmd to a mockCommand. Was: %v", i.cmd)
	}

	expectedCmd := &mockCommand{
		target:            path,
		args:              i.args,
		notifiedOfChanges: false,
	}
	if c.target != expectedCmd.target ||
		!reflect.DeepEqual(c.args, expectedCmd.args) ||
		c.notifiedOfChanges != expectedCmd.notifiedOfChanges {
		t.Errorf("Inequal\nCommand:  %v\nExpected: %v", c, expectedCmd)
	}
}
