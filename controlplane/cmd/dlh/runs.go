package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func runsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "runs",
		Short: "View, follow, or cancel runs",
	}
	c.AddCommand(runsLsCmd(), runsShowCmd(), runsLogsCmd(), runsCancelCmd(), runsReprioritizeCmd())
	return c
}

func runsLsCmd() *cobra.Command {
	var (
		scenario string
		target   string
		status   string
		limit    int
	)
	c := &cobra.Command{
		Use:   "ls",
		Short: "List runs",
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if scenario != "" {
				q.Set("scenario", scenario)
			}
			if target != "" {
				q.Set("target", target)
			}
			if status != "" {
				q.Set("status", status)
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			raw, _, err := newClient().do("GET", "/api/runs", nil, q)
			if err != nil {
				return err
			}
			var resp struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tSCENARIO\tTARGET\tSTATUS\tSTARTED\tSCORE")
			for _, r := range resp.Items {
				started, _ := r["startedAt"].(string)
				score := "—"
				if v, ok := r["score"].(float64); ok {
					score = fmt.Sprintf("%.2f", v)
				}
				fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%s\n",
					r["id"], r["scenario"], stringOrDash(r["target"]), r["status"], started, score)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&scenario, "scenario", "", "Filter by scenario id")
	c.Flags().StringVar(&target, "target", "", "Filter by target id")
	c.Flags().StringVar(&status, "status", "", "Filter by status")
	c.Flags().IntVar(&limit, "limit", 50, "Max rows")
	return c
}

func runsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <runID>",
		Short: "Show a run's detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, _, err := newClient().do("GET", "/api/runs/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			var pretty interface{}
			_ = json.Unmarshal(raw, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func runsLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <runID>",
		Short: "Stream SSE events for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runID := args[0]
			full := strings.TrimRight(flagEndpoint, "/") + "/api/runs/" + url.PathEscape(runID) + "/events"
			req, err := newRequestWithAuth("GET", full)
			if err != nil {
				return err
			}
			resp, err := newClient().http.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue
				}
				fmt.Println(line)
			}
			return scanner.Err()
		},
	}
}

func runsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <runID>",
		Short: "Cancel a running run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, _, err := newClient().do("DELETE", "/api/runs/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			fmt.Println("cancellation requested")
			return nil
		},
	}
}

func runsReprioritizeCmd() *cobra.Command {
	var (
		priority int
		toFront  bool
	)
	c := &cobra.Command{
		Use:   "reprioritize <run-id>",
		Short: "Change a pending run's priority (only works while queued)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := priority
			if toFront {
				p = 1000 // above the Urgent tier (500)
			}
			if p == 0 {
				return fmt.Errorf("provide --priority N or --to-front")
			}
			_, _, err := newClient().do("POST", "/api/runs/"+args[0]+"/priority", map[string]any{"priority": p}, nil)
			if err != nil {
				return err
			}
			fmt.Printf("reprioritized %s → %d\n", args[0], p)
			return nil
		},
	}
	c.Flags().IntVar(&priority, "priority", 0, "New priority")
	c.Flags().BoolVar(&toFront, "to-front", false, "Move to front (priority 1000)")
	return c
}

func stringOrDash(v any) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return "—"
}
