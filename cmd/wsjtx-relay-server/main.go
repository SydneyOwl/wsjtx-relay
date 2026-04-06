package main

import (
	"log"

	"github.com/sydneyowl/wsjtx-relay/internal/server/cli"
)

func main() {
	if err := cli.Execute(nil); err != nil {
		log.Fatalf("%v", err)
	}
}
