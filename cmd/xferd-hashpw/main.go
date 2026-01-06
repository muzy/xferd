package main

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	fmt.Println("xferd Password Hash Generator")
	fmt.Println("==============================")
	fmt.Println()
	fmt.Print("Enter password: ")

	// Read password without echoing to terminal
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}

	if len(password) == 0 {
		fmt.Fprintf(os.Stderr, "Error: password cannot be empty\n")
		os.Exit(1)
	}

	// Generate bcrypt hash with default cost (10)
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating hash: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Generated bcrypt hash:")
	fmt.Println(string(hash))
	fmt.Println()
	fmt.Println("Add this to your config.yml:")
	fmt.Println()
	fmt.Println("server:")
	fmt.Println("  basic_auth:")
	fmt.Println("    enabled: true")
	fmt.Println("    username: your_username")
	fmt.Printf("    password_hash: \"%s\"\n", string(hash))
	fmt.Println()
	fmt.Println("Note: Do NOT use both 'password' and 'password_hash' - use only 'password_hash' for production.")
}
