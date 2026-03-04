package main

import (
	"log"

	"github.com/jfxdev/wireops/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
