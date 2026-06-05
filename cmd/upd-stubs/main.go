package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	var dryRun bool
	var verbose bool
	var prune bool
	// Simple flag parsing (no cobra needed for a tool like this)
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--verbose":
			verbose = true
		case "--prune":
			prune = true
		case "--help", "-h":
			fmt.Println("upd-stubs: generate and update gRPC stub implementations")
			fmt.Println()
			fmt.Println("Usage: upd-stubs [flags]")
			fmt.Println()
			fmt.Println("Flags:")
			fmt.Println("  --dry-run   Render without writing files")
			fmt.Println("  --verbose   Extra diagnostics during generation")
			fmt.Println("  --prune     Annotate orphaned/stale handlers (best-effort)")
			fmt.Println("  --help      Show this help message")
			os.Exit(0)
		}
	}
	if verbose {
		log.Println("running upd-stubs with flags: dry-run=", dryRun, "verbose=", verbose, "prune=", prune)
	}
	services, err := discoverServices(verbose)
	if err != nil {
		log.Fatalf("failed to discover services: %v", err)
	}
	pkgMap := groupServicesByPkg(services)
	// Generate stubs
	for _, sp := range pkgMap {
		if err := generateStubPackage(sp.OutDir, sp.PkgName, sp.Services, dryRun, verbose); err != nil {
			log.Fatalf("failed to generate stubs for %s: %v", sp.OutDir, err)
		}
	}
	// Generate wire.go
	if err := generateWireFile(pkgMap, dryRun, verbose); err != nil {
		log.Fatalf("failed to generate wire.go: %v", err)
	}
	if prune {
		if err := annotateOrphanedMethods(pkgMap, dryRun, verbose); err != nil {
			log.Printf("warning: prune annotation failed: %v", err)
		}
	}
	if dryRun {
		log.Println("dry-run complete, no files were written")
	} else {
		log.Println("stubs generated successfully")
	}
}
