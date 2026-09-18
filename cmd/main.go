package main

import (
	"fmt"
	"os"

	"github.com/mikepjb/spark/internal/view"
)

func main() {

	// view takes interface to call 'backend' i.e LLM
	if err := view.Start(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
