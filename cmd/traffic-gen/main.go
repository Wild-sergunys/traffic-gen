// Package main is the entry point of the traffic-gen tool.
//
// It is currently a placeholder; the real generator will follow.
package main

import "fmt"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	fmt.Printf("traffic-gen %s (commit %s, built %s)\n", version, commit, date)
	fmt.Println("placeholder - real generator coming soon")
}
