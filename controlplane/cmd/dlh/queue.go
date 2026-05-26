package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func queueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Show the per-target-type run queue",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, _, err := newClient().do("GET", "/api/queue", nil, nil)
			if err != nil {
				return err
			}
			var resp struct {
				Lanes []struct {
					Key     string `json:"key"`
					Slots   int    `json:"slots"`
					Running []struct {
						ID, Scenario string `json:"-"`
					} `json:"running"`
					Pending []struct {
						ID       string `json:"id"`
						Scenario string `json:"scenario"`
						Priority *int   `json:"priority"`
					} `json:"pending"`
				} `json:"lanes"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "LANE\tSLOTS\tRUNNING\tQUEUED")
			for _, l := range resp.Lanes {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", l.Key, l.Slots, len(l.Running), len(l.Pending))
			}
			return tw.Flush()
		},
	}
}
