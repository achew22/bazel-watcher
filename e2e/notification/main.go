package main

import (
	"bufio"
	"fmt"
	"os"
)

const successSentinal = "IBAZEL_BUILD_COMPLETED SUCCESS\n"

func main() {
	if os.Getenv("IBAZEL_NOTIFY_CHANGES") != "y" {
		fmt.Printf("Didn't set IBAZEL_NOTIFY_CHANGES to \"y\"\n")
		os.Exit(255)
	}

	fmt.Printf("Started!")

	r := bufio.NewReader(os.Stdin)

	// Listen for events 2 times then quit.
	for i := 0; i < 2; i++ {
		text, err := r.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading stdin: [%s]", err)
			os.Exit(255)
		}
		if text != successSentinal {
			fmt.Printf("Expected success sentinal.\nGot:  %s\nWant: %s\n", text, successSentinal)
			// Trigger test failure
			os.Exit(255)
		}
		fmt.Println("Rebuilt!")
	}
}
