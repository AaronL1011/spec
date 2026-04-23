package main

import (
	"flag"
	"log"
	"os"

	"github.com/nexl/spec-cli/cmd"
	"github.com/spf13/cobra/doc"
)

func main() {
	outputDirectory := flag.String("output", "docs/man", "output directory for generated man pages")
	flag.Parse()

	if err := os.MkdirAll(*outputDirectory, 0o755); err != nil {
		log.Fatalf("creating output directory: %v", err)
	}

	manHeader := &doc.GenManHeader{
		Title:   "SPEC",
		Section: "1",
		Source:  "spec",
		Manual:  "spec manual",
	}

	if err := doc.GenManTree(cmd.RootCmd(), manHeader, *outputDirectory); err != nil {
		log.Fatalf("generating man pages: %v", err)
	}
}
