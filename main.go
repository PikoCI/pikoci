// PikoCI is a lightweight, self-hosted continuous integration server inspired by
// Concourse CI. It uses HCL-based pipeline definitions with resources, jobs, and
// builds as core concepts. This is the main entry point that delegates to the CLI
// command framework.
package main

import (
	"errors"
	"log"
	"os"

	"github.com/xescugc/pikoci/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var exitErr *cmd.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		log.Fatal(err)
	}
}
