package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func scheduleCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "schedule",
		Short: "Manage CronWorkflow-backed schedules",
	}
	c.AddCommand(scheduleCreateCmd(), scheduleLsCmd(), scheduleShowCmd(),
		schedulePauseCmd(), scheduleResumeCmd(), scheduleDeleteCmd())
	return c
}

func scheduleCreateCmd() *cobra.Command {
	var (
		scenario   string
		target     string
		cron       string
		timezone   string
		paramFlags []string
	)
	c := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if scenario == "" || cron == "" {
				return fmt.Errorf("--scenario and --cron are required")
			}
			params := map[string]string{}
			for _, p := range paramFlags {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--param expects key=value, got %q", p)
				}
				params[k] = v
			}
			body := map[string]any{
				"id":         args[0],
				"scenarioId": scenario,
				"cron":       cron,
			}
			if target != "" {
				body["targetId"] = target
			}
			if timezone != "" {
				body["timezone"] = timezone
			}
			if len(params) > 0 {
				body["parameters"] = params
			}
			raw, _, err := newClient().do("POST", "/api/schedules", body, nil)
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
	c.Flags().StringVar(&scenario, "scenario", "", "Scenario WorkflowTemplate name (required)")
	c.Flags().StringVar(&target, "target", "", "Optional remote target ID")
	c.Flags().StringVar(&cron, "cron", "", "5-field cron expression (required)")
	c.Flags().StringVar(&timezone, "timezone", "", "IANA tz name (default UTC)")
	c.Flags().StringArrayVarP(&paramFlags, "param", "p", nil, "Parameter override key=value (repeatable)")
	return c
}

func scheduleLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List schedules",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, _, err := newClient().do("GET", "/api/schedules", nil, nil)
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
			fmt.Fprintln(tw, "ID\tSCENARIO\tTARGET\tCRON\tSUSPENDED\tLAST FIRED\tACTIVE")
			for _, r := range resp.Items {
				lastFired := "—"
				if t, ok := r["lastScheduledAt"].(string); ok && t != "" {
					lastFired = t
				}
				suspended := "false"
				if v, ok := r["suspended"].(bool); ok && v {
					suspended = "true"
				}
				active := "0"
				if v, ok := r["activeCount"].(float64); ok {
					active = fmt.Sprintf("%v", int(v))
				}
				target := "—"
				if v, ok := r["target"].(string); ok && v != "" {
					target = v
				}
				fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
					r["id"], r["scenario"], target, r["cron"], suspended, lastFired, active)
			}
			return tw.Flush()
		},
	}
}

func scheduleShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show schedule detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, _, err := newClient().do("GET", "/api/schedules/"+url.PathEscape(args[0]), nil, nil)
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

// Stubs — replaced in Task 9.
func schedulePauseCmd() *cobra.Command  { return &cobra.Command{Use: "pause", Hidden: true} }
func scheduleResumeCmd() *cobra.Command { return &cobra.Command{Use: "resume", Hidden: true} }
func scheduleDeleteCmd() *cobra.Command { return &cobra.Command{Use: "delete", Hidden: true} }
