// Command v6ctl is the operator CLI of the whynoipv6 backend.
package main

import (
	"fmt"
	"os"

	"github.com/lasseh/whynoipv6/internal/config"
)

func main() {
	cfg, err := config.Load("v6ctl")
	if err != nil {
		fmt.Fprintln(os.Stderr, "v6ctl: "+err.Error())
		os.Exit(1)
	}
	log := cfg.InstallLogger()
	cfg.LogSummary(log)
}
