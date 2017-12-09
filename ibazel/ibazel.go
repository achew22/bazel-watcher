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

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bazelbuild/bazel-watcher/bazel"
	"github.com/bazelbuild/bazel-watcher/ibazel/command"
	"github.com/bazelbuild/bazel-watcher/ibazel/live_reload"
	"github.com/bazelbuild/bazel-watcher/ibazel/profiler"
	"github.com/bazelbuild/bazel-watcher/ibazel/query"
	"github.com/bazelbuild/bazel-watcher/ibazel/watch"

	blaze_query "github.com/bazelbuild/bazel-watcher/third_party/bazel/master/src/main/protobuf"
)

var osExit = os.Exit
var bazelNew = bazel.New
var commandDefaultCommand = command.DefaultCommand
var commandNotifyCommand = command.NotifyCommand

type State string
type runnableCommand func(...string) error

const (
	DEBOUNCE_QUERY State = "DEBOUNCE_QUERY"
	QUERY          State = "QUERY"
	WAIT           State = "WAIT"
	DEBOUNCE_RUN   State = "DEBOUNCE_RUN"
	RUN            State = "RUN"
	QUIT           State = "QUIT"
)

const sourceQuery = "kind('source file', deps(set(%s)))"
const buildQuery = "buildfiles(deps(set(%s)))"

type IBazel struct {
	debounceDuration time.Duration

	cmd       command.Command
	args      []string
	bazelArgs []string

	sigs           chan os.Signal // Signals channel for the current process
	interruptCount int

	lifecycleListeners []Lifecycle

	workspaceFinder WorkspaceFinder
	querier         Querier
	watcher         Watcher

	state State
}

func New(wsf WorkspaceFinder) (*IBazel, error) {
	i := &IBazel{}
	err := i.setup()
	if err != nil {
		return nil, err
	}

	// Default at 100ms (overridable by SetDebounceDuration).
	i.debounceDuration = 100 * time.Millisecond
	i.workspaceFinder = wsf

	workspacePath, err := wsf.FindWorkspace()
	if err != nil {
		return nil, err
	}
	i.querier = query.New(bazelNew, workspacePath)

	i.sigs = make(chan os.Signal, 1)
	signal.Notify(i.sigs, syscall.SIGINT, syscall.SIGTERM)

	liveReload := live_reload.New()
	profiler := profiler.New(Version)

	liveReload.AddEventsListener(profiler)

	i.lifecycleListeners = []Lifecycle{
		liveReload,
		profiler,
	}

	info, _ := i.getInfo()
	for _, l := range i.lifecycleListeners {
		l.Initialize(info)
	}

	go func() {
		for {
			i.handleSignals()
		}
	}()

	return i, nil
}

func (i *IBazel) handleSignals() {
	// Got an OS signal (SIGINT, SIGTERM).
	sig := <-i.sigs

	switch sig {
	case syscall.SIGINT:
		if i.cmd != nil && i.cmd.IsSubprocessRunning() {
			fmt.Fprintf(os.Stderr, "\nSubprocess killed from getting SIGINT\n")
			i.cmd.Terminate()
		} else {
			osExit(3)
		}
		break
	case syscall.SIGTERM:
		if i.cmd != nil && i.cmd.IsSubprocessRunning() {
			fmt.Fprintf(os.Stderr, "\nSubprocess killed from getting SIGTERM\n")
			i.cmd.Terminate()
		}
		osExit(3)
		return
	default:
		fmt.Fprintf(os.Stderr, "Got a signal that wasn't handled. Please file a bug against bazel-watcher that describes how you did this. This is a big problem.\n")
	}

	i.interruptCount += 1
	if i.interruptCount > 2 {
		fmt.Fprintf(os.Stderr, "\nExiting from getting SIGINT 3 times\n")
		osExit(3)
	}
}

func (i *IBazel) newBazel() bazel.Bazel {
	b := bazelNew()
	b.SetArguments(i.bazelArgs)
	return b
}

func (i *IBazel) SetBazelArgs(args []string) {
	i.bazelArgs = args
}

func (i *IBazel) SetDebounceDuration(debounceDuration time.Duration) {
	i.debounceDuration = debounceDuration
}

func (i *IBazel) Cleanup() {
	for _, l := range i.lifecycleListeners {
		l.Cleanup()
	}
	i.watcher.Cleanup()
}

func (i *IBazel) targetDecider(target string, rule *blaze_query.Rule) {
	for _, l := range i.lifecycleListeners {
		// TODO: As the name implies, it would be good to use this to make a
		// determination about if future events should be routed to this listener.
		// Why not do it now?
		// Right now I don't track which file is associated with the end target. I
		// just query for a list of all files that are rdeps of any target that is
		// in the list of targets to build/test/run (although run can only have 1).
		// Since I don't have that mapping right now the information doesn't
		// presently exist to implement this properly. Additionally, since querying
		// is currently in the critical path for getting something the user cares
		// about on screen, I'm not sure that it is wise to do this in the first
		// pass. It might be worth triggering the user action, launching their thing
		// and then running a background thread to access the data.
		l.TargetDecider(rule)
	}
}

func (i *IBazel) changeDetected(targets []string, changeType string, change string) {
	for _, l := range i.lifecycleListeners {
		l.ChangeDetected(targets, changeType, change)
	}
}

func (i *IBazel) beforeCommand(targets []string, command string) {
	for _, l := range i.lifecycleListeners {
		l.BeforeCommand(targets, command)
	}
}

func (i *IBazel) afterCommand(targets []string, command string, success bool) {
	for _, l := range i.lifecycleListeners {
		l.AfterCommand(targets, command, success)
	}
}

func (i *IBazel) setup() error {
	var err error
	i.watcher, err = watch.NewFSNotifyWatcher()
	return err
}

// Run the specified target (singular) in the IBazel loop.
func (i *IBazel) Run(target string, args []string) error {
	i.args = args
	return i.loop("run", i.run, []string{target})
}

// Build the specified targets in the IBazel loop.
func (i *IBazel) Build(targets ...string) error {
	return i.loop("build", i.build, targets)
}

// Test the specified targets in the IBazel loop.
func (i *IBazel) Test(targets ...string) error {
	return i.loop("test", i.test, targets)
}

func (i *IBazel) loop(command string, commandToRun runnableCommand, targets []string) error {
	joinedTargets := strings.Join(targets, " ")

	i.state = QUERY
	for {
		i.iteration(command, commandToRun, targets, joinedTargets)
	}

	return nil
}

func (i *IBazel) iteration(command string, commandToRun runnableCommand, targets []string, joinedTargets string) {
	fmt.Fprintf(os.Stderr, "State: %s\n", i.state)
	switch i.state {
	case WAIT:
		select {
		case e := <-i.watcher.SourceEvents():
			fmt.Fprintf(os.Stderr, "Changed: %q. Rebuilding...\n", e.Name)
			i.changeDetected(targets, "source", e.Name)
			i.state = DEBOUNCE_RUN
		case e := <-i.watcher.BuildEvents():
			fmt.Fprintf(os.Stderr, "Build graph changed: %q. Requerying...\n", e.Name)
			i.changeDetected(targets, "graph", e.Name)
			i.state = DEBOUNCE_QUERY
		}
	case DEBOUNCE_QUERY:
		select {
		case <-i.watcher.BuildEvents():
			i.state = DEBOUNCE_QUERY
		case <-time.After(i.debounceDuration):
			i.state = QUERY
		}
	case QUERY:
		// Query for which files to watch.
		fmt.Fprintf(os.Stderr, "Querying for BUILD files...\n")
		query := fmt.Sprintf(buildQuery, joinedTargets)
		toWatch, err := i.querier.QueryForSourceFiles(query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error querying for source files: %s\n", err)
			osExit(4)
		}
		buildCount, err := i.watcher.WatchBuildFiles(toWatch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error querying for source files: %s\n", err)
			osExit(4)
		}

		fmt.Fprintf(os.Stderr, "Querying for source files...\n")
		query = fmt.Sprintf(sourceQuery, joinedTargets)
		toWatch, err = i.querier.QueryForSourceFiles(query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error querying for source files: %s\n", err)
			osExit(4)
		}
		sourceCount, err := i.watcher.WatchSourceFiles(toWatch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error querying for source files: %s\n", err)
			osExit(4)
		}

		fmt.Fprintf(os.Stderr, "Watching %d BUILD files and %d source files\n", buildCount, sourceCount)

		i.state = RUN
	case DEBOUNCE_RUN:
		select {
		case <-i.watcher.SourceEvents():
			i.state = DEBOUNCE_RUN
		case <-time.After(i.debounceDuration):
			i.state = RUN
		}
	case RUN:
		fmt.Fprintf(os.Stderr, "%sing %s\n", strings.Title(command), joinedTargets)
		i.beforeCommand(targets, command)
		err := commandToRun(targets...)
		i.afterCommand(targets, command, err == nil)
		i.state = WAIT
	}
}

func (i *IBazel) build(targets ...string) error {
	b := i.newBazel()

	b.Cancel()
	b.WriteToStderr(true)
	b.WriteToStdout(true)
	err := b.Build(targets...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build error: %v\n", err)
		return err
	}
	return nil
}

func (i *IBazel) test(targets ...string) error {
	b := i.newBazel()

	b.Cancel()
	b.WriteToStderr(true)
	b.WriteToStdout(true)
	err := b.Test(targets...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build error: %v\n", err)
		return err
	}
	return nil
}

func contains(l []string, e string) bool {
	for _, i := range l {
		if i == e {
			return true
		}
	}
	return false
}

func (i *IBazel) setupRun(target string) command.Command {
	rule, err := i.querier.QueryRule(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		osExit(4)
	}

	i.targetDecider(target, rule)

	commandNotify := false
	for _, attr := range rule.Attribute {
		if *attr.Name == "tags" && *attr.Type == blaze_query.Attribute_STRING_LIST {
			if contains(attr.StringListValue, "ibazel_notify_changes") {
				commandNotify = true
			}
		}
	}

	if commandNotify {
		fmt.Fprintf(os.Stderr, "Launching with notifications\n")
		return commandNotifyCommand(i.bazelArgs, target, i.args)
	} else {
		return commandDefaultCommand(i.bazelArgs, target, i.args)
	}
}

func (i *IBazel) run(targets ...string) error {
	if i.cmd == nil {
		// If the command is empty, we are in our first pass through the state
		// machine and we need to make a command object.
		i.cmd = i.setupRun(targets[0])
		err := i.cmd.Start()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Run start failed %v\n", err)
		}
		return err
	}

	fmt.Fprintf(os.Stderr, "Notifying of changes\n")
	i.cmd.NotifyOfChanges()
	return nil
}

func (i *IBazel) getInfo() (*map[string]string, error) {
	b := i.newBazel()

	res, err := b.Info()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting Bazel info %v\n", err)
		return nil, err
	}

	return &res, nil
}
