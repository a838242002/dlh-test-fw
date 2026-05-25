package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	var (
		paramFlags []string
		wait       bool
		target     string
		priority   int
	)
	c := &cobra.Command{
		Use:   "run <scenario>",
		Short: "Submit a scenario",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			scenario := args[0]
			params := map[string]string{}
			for _, p := range paramFlags {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--param expects key=value, got %q", p)
				}
				params[k] = v
			}
			client := newClient()
			body := map[string]any{"scenarioId": scenario}
			if target != "" {
				body["targetId"] = target
			}
			if priority != 0 {
				body["priority"] = priority
			}
			if len(params) > 0 {
				body["parameters"] = params
			}
			respBody, _, err := client.do("POST", "/api/runs", body, nil)
			if err != nil {
				return err
			}
			var run map[string]any
			if err := json.Unmarshal(respBody, &run); err != nil {
				return err
			}
			runID, _ := run["id"].(string)
			fmt.Printf("submitted: %s\n", runID)
			if !wait {
				return nil
			}
			return waitForRun(client, runID)
		},
	}
	c.Flags().StringArrayVarP(&paramFlags, "param", "p", nil, "Parameter override key=value (repeatable)")
	c.Flags().BoolVar(&wait, "wait", false, "Block until the run reaches a terminal phase")
	c.Flags().StringVar(&target, "target", "", "Optional remote target ID")
	c.Flags().IntVar(&priority, "priority", 0, "Workflow priority override (0 = scenario default)")
	return c
}

func waitForRun(client *apiClient, runID string) error {
	for {
		raw, _, err := client.do("GET", "/api/runs/"+url.PathEscape(runID), nil, nil)
		if err != nil {
			return err
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		status, _ := m["status"].(string)
		fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), status)
		switch status {
		case "Succeeded", "Failed", "Error":
			return nil
		}
		time.Sleep(5 * time.Second)
	}
}
