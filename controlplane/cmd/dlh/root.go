package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagEndpoint string
	flagToken    string
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dlh",
		Short: "dlh — controlplane CLI for dlh-test-fw",
		Long:  "Submit scenarios, view runs, and stream events against the dlh-controlplane API.",
	}
	root.PersistentFlags().StringVar(&flagEndpoint, "endpoint", endpointDefault(), "Controlplane base URL")
	root.PersistentFlags().StringVar(&flagToken, "token", tokenDefault(), "OIDC bearer token (or set DLH_TOKEN)")
	root.AddCommand(runCmd(), runsCmd(), loginCmd(), scheduleCmd())
	return root
}

func endpointDefault() string {
	if v := os.Getenv("DLH_ENDPOINT"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func tokenDefault() string {
	if v := os.Getenv("DLH_TOKEN"); v != "" {
		return v
	}
	return loadCachedToken()
}


