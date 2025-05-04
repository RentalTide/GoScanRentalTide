//go:build windows
// +build windows

package main

import (
    "fmt"
    "os"
)

func init() {
    // Check if running as admin
    if !isAdmin() {
        fmt.Println("This application requires administrator privileges to access scanner hardware.")
        fmt.Println("Please right-click the executable and select 'Run as administrator'.")
        
        // Wait for user input before exiting
        fmt.Println("\nPress Enter to exit...")
        fmt.Scanln()
        os.Exit(1)
    }
}

func isAdmin() bool {
    _, err := os.Open("\\\\.\\PHYSICALDRIVE0")
    return err == nil
}
