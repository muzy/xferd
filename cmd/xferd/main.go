package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/muzy/xferd/internal/service"
)

const version = "1.0.0"

func main() {
	// Command line flags
	configPath := flag.String("config", "/etc/xferd/config.yml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	// Show version
	if *showVersion {
		fmt.Printf("xferd version %s\n", version)
		os.Exit(0)
	}

	// Setup logging
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Printf("Starting xferd v%s", version)

	// Run service
	if err := service.Run(*configPath); err != nil {
		log.Fatalf("Service error: %v", err)
	}
}

