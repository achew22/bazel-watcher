package e2e

import (
	"testing"
	"time"
)

func TestNotificationRun(t *testing.T) {
	ibazel := IBazelTester("//e2e/notification", "e2e/notification/notification")
	ibazel.Run()
	defer ibazel.Kill()

	time.Sleep(100 * time.Millisecond)
	res := ibazel.GetOutput()

	assertEqual(t, "Notification", res, "Ouput was inequal")
}

func TestNotificationRunWithModifiedFile(t *testing.T) {
	t.Skip()
	ibazel := IBazelTester("//e2e/notification", "e2e/notification/notification")
	ibazel.Run()
	defer ibazel.Kill()

	// Give it 100 ms to start up before throwing to the hounds.
	time.Sleep(100 * time.Millisecond)

	// Used as a buffer to append expected output to.
	expectedOut := ""
	pid := ibazel.GetSubprocessPid()
	count := 0
	verify := func() {
		time.Sleep(500 * time.Millisecond)

		count += 1

		p := ibazel.GetSubprocessPid()
		if pid != p {
			t.Errorf("Subsequent runs of the notify command should have the same pid. Was: %v, Now: %v", pid, p)
		}

		assertEqual(t, expectedOut, ibazel.GetOutput(), "Ouput was inequal")
	}

	// Give it time to start up and query.
	expectedOut += "Started!"
	verify()

	// Manipulate a source file and sleep past the debounce.
	expectedOut += "Rebuilt!"
	manipulateSourceFile(count)
	verify()

	// Now a BUILD file.
	expectedOut += "Rebuilt!"
	manipulateBUILDFile(count)
	verify()
}
