package main

import (
	"os"
	"github.com/pip-services3-go/pip-services3-container-go/v3/examples"
)

func main() {
	process := examples.NewDummyProcess()
	process.Run(os.Args)
}