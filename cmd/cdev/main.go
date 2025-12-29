// Package main is the entry point for cdev.
//
//	@title			cdev API
//	@version		1.0
//	@description	Mobile AI Coding Monitor & Controller Agent API.
//	@description	Enables remote monitoring and control of AI coding agents from mobile devices.
//
//	@contact.name	Brian Ly
//	@contact.url	https://github.com/brianly1003/cdev
//
//	@license.name	MIT
//	@license.url	https://opensource.org/licenses/MIT
//
//	@host		localhost:8766
//	@BasePath	/
//	@schemes	http
//
//	@tag.name			health
//	@tag.description	Health check endpoints
//	@tag.name			status
//	@tag.description	Agent status endpoints
//	@tag.name			claude
//	@tag.description	Claude CLI management endpoints
//	@tag.name			git
//	@tag.description	Git operations endpoints
//	@tag.name			file
//	@tag.description	File operations endpoints
//	@tag.name			images
//	@tag.description	Image upload and management for Claude vision analysis
//	@tag.name			repository
//	@tag.description	Repository file browsing, search, and indexing
package main

import (
	"fmt"
	"os"

	"github.com/brianly1003/cdev/cmd/cdev/cmd"

	_ "github.com/brianly1003/cdev/api/swagger" // swagger docs
)

// Version information (set by ldflags during build)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Pass version info to cmd package
	cmd.SetVersionInfo(Version, BuildTime, GitCommit)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
