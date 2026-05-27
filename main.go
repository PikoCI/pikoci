package main

import (
	"errors"
	"log"
	"os"

	"github.com/pikoci/pikoci/cmd"
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
