package iutput_runner

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bazelbuild/bazel-watcher/internal/e2e"
)

const mainFiles = `
-- WORKSPACE --
#required
load("//repo:deps.bzl", "deps")
deps(name = "hash")
load("@hash//:build.bzl", "build")
-- repo/deps.bzl --
def _deps_impl(rctx):
    rctx.file("BUILD")
    rctx.file("build.bzl", "build = 1")
    return {"name": rctx.attr.name, "integrity": "sha256-aaaa"}

deps = repository_rule(implementation = _deps_impl, attrs = {"integrity": attr.string()})
-- repo/BUILD --
-- single/defs.bzl --
def fix_deps(name):
    print("used-to-be-the-magic-command")
-- single/BUILD --
load("//single:defs.bzl", "fix_deps")

genrule(name="gen_overwrite", srcs=["overwrite.sh"], outs=["outrun1"],cmd="echo runacommand >&2 && cat $< > $@")

sh_binary(
  name = "test",
  srcs = ["test.sh"],
)

sh_binary(
  name = "overwrite",
  srcs = [":gen_overwrite"],
)
-- single/test.sh --
printf "action"
-- single/overwrite.sh --
printf "overwrite"
-- multiple/BUILD --
# maybe I can dict-comprehension later
genrule(name="fix_deps", srcs=["test.sh"], outs=["out-fixdeps"],cmd="echo runcommand foo >&2 && echo runcommand bar >&2 && echo runcommand foo >&2 && echo runcommand baz >&2 && cat $< > $@")

sh_binary(
  name = "test",
  srcs = ["test.sh"],
)
genrule(
  name = "collector",
  cmd = "echo 'date' > $@",
  executable = True,
  outs = ["out-collector"],
  srcs = [ ":fix_deps" ]
)
-- multiple/test.sh --
printf "action"
`

func TestMain(m *testing.M) {
	e2e.TestMain(m, e2e.Args{
		Main: mainFiles,
	})
}

func checkNoSentinel(t *testing.T, sentinelFile *os.File, msg string) {
	t.Helper()

	if _, err := os.Stat(sentinelFile.Name()); !os.IsNotExist(err) {
		t.Errorf("Found a sentinel when expecting none: %s\n", msg)
	}
}

func checkSentinel(t *testing.T, sentinelFile *os.File, msg string) {
	t.Helper()
	sentinalFileName := sentinelFile.Name()

	deadline := time.Now().Add(5 * time.Second)
	var err error
	for {
		if time.Now().After(deadline) {
			t.Errorf("Couldn't find sentinal. os.Stat(%q): %s\n%s\n", sentinalFileName, err, msg)
			return
		} else if _, err := os.Stat(sentinalFileName); err == nil {
			// No error stat'ing the file means it exists.
			os.Remove(sentinelFile.Name())
			return
		}
	}
}

func TestOutputRunner(t *testing.T) {
	sentinelFile, err := ioutil.TempFile("", "fixCommandSentinel")
	if err != nil {
		t.Errorf("ioutil.TempFile(\"\", \"fixCommandSentinel\": %v", err)
	}
	sentinalFileName := strings.Replace(sentinelFile.Name(), "\\", "/", -1)

	e2e.Must(t, sentinelFile.Close())
	checkSentinel(t, sentinelFile, "ioutil.TempFile creates the file by default. Delete it.")
	checkNoSentinel(t, sentinelFile, "The sentinal should now be deleted.")

	// First check that it doesn't run if there isn't a `.bazel_fix_commands.json` file.
	ibazel := e2e.SetUp(t)
        t.Log("Running //single:overwrite with no bazel_fix_commands")
	ibazel.RunWithBazelFixCommands("//single:overwrite")

	// Ensure it prints out the banner.
	ibazel.ExpectIBazelError("Did you know", 50 * time.Second)

	ibazel.Kill()

	ibazel = e2e.SetUp(t)
        t.Log("Creating bazel_fix_commands")
      e2e.MustWriteFile(t, ".bazel_fix_commands.json", fmt.Sprintf(`
      [{
              "regex": "INFO: Build completed successfully",
              "command": "touch",
              "args": ["%s"]
      }]`, sentinalFileName))

        t.Log("overwriting overwrite.sh")
	e2e.MustWriteFile(t, "single/overwrite.sh", `
printf "overwrite1"
`)

        t.Log("Running //single:overwrite with bazel_fix_commands")
	ibazel.RunWithBazelFixCommands("//single:overwrite")

	ibazel.ExpectOutput("overwrite1")
	checkSentinel(t, sentinelFile, "The first run should create a sentinel.")

	ibazel.Kill()

	// Invoke the test a 2nd time to ensure it works over multiple separate
	// invocations of ibazel.
	ibazel = e2e.SetUp(t)

	// additional change in dependency of the genrule() so that the side-effect printed text is triggered
	e2e.MustWriteFile(t, "single/overwrite.sh", `
printf "overwrite3"
`)
	ibazel.RunWithBazelFixCommands("//single:overwrite")
	ibazel.ExpectOutput("overwrite3")
	checkSentinel(t, sentinelFile, "The second run should create a sentinel.")

	// TODO: Figure out why the 2nd invocation doesn't touch the file.
	// Test that the command is run again.
	//e2e.MustWriteFile(t, "overwrite.sh", `printf "overwrite2"`)

	//ibazel.ExpectOutput("overwrite2")
	//checkSentinel(t, sentinelFile, "The third run should create a sentinel.")

	// Now replace the print and it shouldn't fire.
        // More recent trick/fact: not updating overwrite.sh means no source deps changed
	e2e.MustWriteFile(t, "defs.bzl", `
def fix_deps():
  print("not it")
`)

	ibazel.ExpectOutput("overwrite3")
	checkNoSentinel(t, sentinelFile, "The third run should not create a sentinel.")
}

func TestOutputRunnerUniqueCommandsOnly(t *testing.T) {
        // TODO: Change this to validate that execution happens based on the executables defined in main instead of on the output of Bazel itself
        e2e.MustWriteFile(t, ".bazel_fix_commands.json", `
       [{
               "regex": "INFO: Found 1 target",
               "command": "echo",
               "args": ["foo"]
       }, {
               "regex": "bazel-bin/multiple/out-collector",
               "command": "echo",
               "args": ["baz"]
       }, {
               "regex": "INFO: Build completed successfully",
               "command": "echo",
               "args": ["bar"]
       }]`)

      ibazel := e2e.NewIBazelTester(t)
      ibazel.RunWithBazelFixCommands("//multiple:collector")
      defer ibazel.Kill()

      ibazel.ExpectFixCommands([]string{
              "echo foo",
              "echo baz",
              "echo bar",
      })
}

func TestOutputRunnerTriggerOnRepoRule(t *testing.T) {
	e2e.MustWriteFile(t, ".bazel_fix_commands.json", `
		[{
					"regex": "Rule '([^']+)' indicated that a canonical reproducible form can be obtained by modifying arguments ([^ ]+) = \"([^\"]+)\"",
					"command": "echo",
					"args": ["$1", "$2", "$3"]

		}]
  `)
	ibazel := e2e.SetUp(t)
	ibazel.Clean()
	ibazel.RunUnverifiedWithBazelFixCommands("//single:overwrite")
	defer ibazel.Kill()

	ibazel.ExpectFixCommands([]string{
					"echo hash integrity sha256-aaaa",
	})
}

func TestNotifyWhenInvalidConfig(t *testing.T) {
	e2e.MustWriteFile(t, ".bazel_fix_commands.json", `
invalid json file
`)

	ibazel := e2e.SetUp(t)
	ibazel.RunWithBazelFixCommands("//single:test")
	defer ibazel.Kill()

	// It should run the program and print out an error that says your JSON is
	// invalid.
	ibazel.ExpectIBazelError("Error in .bazel_fix_commands.json")
	ibazel.ExpectOutput("action")
}

func TestMergeGlobalAndLocalConfigs(t *testing.T) {
	// Create a temporary home directory with a global config
	tmpHome, err := ioutil.TempDir("", "ibazel-test-home")
	if err != nil {
		t.Fatalf("Failed to create temp home directory: %v", err)
	}
	defer os.RemoveAll(tmpHome)

	// Save original HOME and restore it after the test
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpHome)

	// Create global config with a command that matches a specific pattern
	globalConfigContent := `[{
		"regex": "global-trigger",
		"command": "echo",
		"args": ["global-command"]
	}]`
	globalConfigPath := tmpHome + "/.bazel_fix_commands.json"
	if err := ioutil.WriteFile(globalConfigPath, []byte(globalConfigContent), 0644); err != nil {
		t.Fatalf("Failed to write global config: %v", err)
	}

	// Create local config with a different command
	e2e.MustWriteFile(t, ".bazel_fix_commands.json", `[{
		"regex": "local-trigger",
		"command": "echo",
		"args": ["local-command"]
	}]`)

	ibazel := e2e.SetUp(t)

	// Create a genrule that outputs both triggers
	e2e.MustWriteFile(t, "merge_test/BUILD", `
genrule(
  name = "test_merge",
  outs = ["out.txt"],
  cmd = "echo 'global-trigger' >&2 && echo 'local-trigger' >&2 && echo 'done' > $@"
)`)

	ibazel.RunWithBazelFixCommands("//merge_test:test_merge")
	defer ibazel.Kill()

	// Both commands should be executed
	ibazel.ExpectFixCommands([]string{
		"echo global-command",
		"echo local-command",
	})
}

func TestGlobalConfigOnly(t *testing.T) {
	// Create a temporary home directory with a global config
	tmpHome, err := ioutil.TempDir("", "ibazel-test-home")
	if err != nil {
		t.Fatalf("Failed to create temp home directory: %v", err)
	}
	defer os.RemoveAll(tmpHome)

	// Save original HOME and restore it after the test
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpHome)

	// Create global config
	globalConfigContent := `[{
		"regex": "INFO: Build completed successfully",
		"command": "echo",
		"args": ["from-global"]
	}]`
	globalConfigPath := tmpHome + "/.bazel_fix_commands.json"
	if err := ioutil.WriteFile(globalConfigPath, []byte(globalConfigContent), 0644); err != nil {
		t.Fatalf("Failed to write global config: %v", err)
	}

	// Do NOT create a local config - test that global config works alone
	ibazel := e2e.SetUp(t)
	ibazel.RunWithBazelFixCommands("//single:test")
	defer ibazel.Kill()

	// The global config command should be executed
	ibazel.ExpectFixCommands([]string{
		"echo from-global",
	})
}
