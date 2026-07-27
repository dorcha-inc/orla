package orlaclient

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/harvard-cns/orla/internal/wire"
)

// NewRootCmd builds the orlactl command tree. The daemon address comes
// from --addr, falling back to ORLA_ADDR and then localhost:8081.
func NewRootCmd() *cobra.Command {
	var addr string
	root := &cobra.Command{
		Use:           "orlactl",
		Short:         "Command-line client for the orla daemon",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&addr, "addr",
		cmp.Or(os.Getenv("ORLA_ADDR"), "http://localhost:8081"), "orla daemon address")

	client := func() *Client { return New(addr) }
	root.AddCommand(newBackendCmd(client), newStageCmd(client), newMappingCmd(client), newSchedulerCmd(client), newCostsCmd(client), newFeedbackCmd(client))
	return root
}

func newBackendCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "backend", Short: "Manage backends"}
	cmd.AddCommand(
		newBackendCreateCmd(client),
		newBackendListCmd(client),
		newBackendGetCmd(client),
		newBackendRmCmd(client),
	)
	return cmd
}

func newBackendCreateCmd(client func() *Client) *cobra.Command {
	var req wire.CreateBackendRequest
	var inCost, outCost, quality, rate float64
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Register a backend",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()
			if f.Changed("input-cost") {
				req.InputCostPerMtoken = &inCost
			}
			if f.Changed("output-cost") {
				req.OutputCostPerMtoken = &outCost
			}
			if f.Changed("quality") {
				req.Quality = &quality
			}
			if f.Changed("rate") {
				req.RatePerSecond = &rate
			}
			b, err := client().CreateBackend(cmd.Context(), req)
			if err != nil {
				return err
			}
			fmt.Printf("registered backend %q -> %s\n", b.Name, cmp.Or(ptr(b.ModelID), "(tool)"))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Name, "name", "", "backend name (required)")
	f.StringVar(&req.Endpoint, "endpoint", "", "OpenAI-compatible base URL (required)")
	f.StringVar(&req.ModelID, "model", "", "provider-prefixed model id, e.g. ollama:qwen2.5:0.5b")
	f.StringVar(&req.APIKeyEnvVar, "api-key-env", "", "env var orla reads the API key from")
	f.StringVar(&req.CostSource, "cost-source", "", "URL orla polls for the backend's current costs")
	f.Int32Var(&req.MaxConcurrency, "max-concurrency", 1, "max concurrent requests")
	f.StringVar(&req.Kind, "kind", "", "backend kind: llm (default) or tool")
	f.StringVar(&req.ToolKind, "tool-kind", "", "tool kind, for kind=tool")
	f.Float64Var(&inCost, "input-cost", 0, "input cost per million tokens")
	f.Float64Var(&outCost, "output-cost", 0, "output cost per million tokens")
	f.Float64Var(&quality, "quality", 0, "quality prior")
	f.Float64Var(&rate, "rate", 0, "requests per second cap")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("endpoint")
	return cmd
}

func newBackendListCmd(client func() *Client) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List backends",
		RunE: func(cmd *cobra.Command, _ []string) error {
			bs, err := client().ListBackends(cmd.Context())
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(bs)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tKIND\tMODEL\tCIRCUIT")
			for _, b := range bs {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", b.Name, b.Kind, cmp.Or(ptr(b.ModelID), "-"), cmp.Or(b.CircuitBreaker, "-"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table or json")
	return cmd
}

func newBackendGetCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "get NAME",
		Short: "Show one backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := client().GetBackend(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(b)
		},
	}
}

func newBackendRmCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Remove a backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := client().DeleteBackend(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("removed backend %q\n", args[0])
			return nil
		},
	}
}

func newSchedulerCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "scheduler", Short: "Manage the scheduling policy"}
	policy := &cobra.Command{Use: "policy", Short: "Manage the scheduling policy"}
	policy.AddCommand(
		newSchedulerPolicyShowCmd(client),
		newSchedulerPolicySetCmd(client),
		newSchedulerPolicyDisableCmd(client),
	)
	cmd.AddCommand(policy)
	return cmd
}

func newSchedulerPolicyShowCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the active scheduling policy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().GetSchedulerPolicy(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(p)
		},
	}
}

func newSchedulerPolicySetCmd(client func() *Client) *cobra.Command {
	var (
		policyURL string
		timeoutMS int
	)
	cmd := &cobra.Command{
		Use:   "set --url URL [--timeout-ms N]",
		Short: "Point the scheduler at an external policy service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().SetSchedulerPolicy(cmd.Context(), wire.SchedulerPolicy{
				URL:       policyURL,
				TimeoutMS: timeoutMS,
			})
			if err != nil {
				return err
			}
			fmt.Printf("scheduling policy set: url=%s timeout_ms=%d\n", p.URL, p.TimeoutMS)
			return nil
		},
	}
	cmd.Flags().StringVar(&policyURL, "url", "", "scheduling service URL (required)")
	cmd.Flags().IntVar(&timeoutMS, "timeout-ms", 0, "per-decision timeout in milliseconds (default 50)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func newSchedulerPolicyDisableCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Revert to first-come-first-served scheduling",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := client().SetSchedulerPolicy(cmd.Context(), wire.SchedulerPolicy{URL: ""}); err != nil {
				return err
			}
			fmt.Println("scheduling policy disabled, serving first-come-first-served")
			return nil
		},
	}
}

func newStageMapperCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "mapper", Short: "Manage the dynamic stage mapper"}
	cmd.AddCommand(
		newStageMapperShowCmd(client),
		newStageMapperSetCmd(client),
		newStageMapperDisableCmd(client),
	)
	return cmd
}

func newStageMapperShowCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the active dynamic stage mapper",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().GetStageMapper(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(p)
		},
	}
}

func newStageMapperSetCmd(client func() *Client) *cobra.Command {
	var (
		mapperURL string
		timeoutMS int
	)
	cmd := &cobra.Command{
		Use:   "set --url URL [--timeout-ms N]",
		Short: "Route stages through an external mapper service, per request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().SetStageMapper(cmd.Context(), wire.StageMapper{
				URL:       mapperURL,
				TimeoutMS: timeoutMS,
			})
			if err != nil {
				return err
			}
			fmt.Printf("stage mapper set: url=%s timeout_ms=%d\n", p.URL, p.TimeoutMS)
			return nil
		},
	}
	cmd.Flags().StringVar(&mapperURL, "url", "", "mapper service URL (required)")
	cmd.Flags().IntVar(&timeoutMS, "timeout-ms", 0, "per-decision timeout in milliseconds (default 50)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func newStageMapperDisableCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Revert to static stage routing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := client().SetStageMapper(cmd.Context(), wire.StageMapper{URL: ""}); err != nil {
				return err
			}
			fmt.Println("stage mapper disabled, stages route by their static mapping")
			return nil
		},
	}
}

func newCostsCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "costs", Short: "Manage cost polling"}
	policy := &cobra.Command{Use: "policy", Short: "Manage the cost policy"}
	policy.AddCommand(
		newCostPolicyShowCmd(client),
		newCostPolicySetCmd(client),
	)
	cmd.AddCommand(policy)
	return cmd
}

func newCostPolicyShowCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the active cost policy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().GetCostPolicy(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(p)
		},
	}
}

func newCostPolicySetCmd(client func() *Client) *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "set --refresh-interval DURATION",
		Short: "Set how often orla refreshes prices from cost sources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := client().SetCostPolicy(cmd.Context(), wire.CostPolicy{
				RefreshIntervalMS: int(interval.Milliseconds()),
			})
			if err != nil {
				return err
			}
			fmt.Printf("cost policy set: refresh_interval=%s\n",
				time.Duration(p.RefreshIntervalMS)*time.Millisecond)
			return nil
		},
	}
	cmd.Flags().DurationVar(&interval, "refresh-interval", 0, "time between cost refreshes, e.g. 30s (required)")
	_ = cmd.MarkFlagRequired("refresh-interval")
	return cmd
}

func newStageCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "stage", Short: "Manage stage mappings"}
	cmd.AddCommand(
		newStageMapCmd(client),
		newStageMapperCmd(client),
		newStagePromptCmd(client),
		newStageCaptureCmd(client),
		newStageListCmd(client),
		newStageGetCmd(client),
		newStageRmCmd(client),
	)
	return cmd
}

func newStageCaptureCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "capture STAGE {on|off}",
		Short: "Toggle per-stage request and response capture",
		Long: "Turn capture_io on or off for a stage. When on, the proxy stores " +
			"the request and response content of every call tagged with the stage " +
			"in the completion_io table, readable per workflow run. Off by default.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var on bool
			switch args[1] {
			case "on":
				on = true
			case "off":
				on = false
			default:
				return fmt.Errorf("second argument must be on or off, got %q", args[1])
			}
			s, err := client().PatchStage(cmd.Context(), args[0], wire.PatchStageRequest{CaptureIO: &on})
			if err != nil {
				return err
			}
			state := "off"
			if s.CaptureIO {
				state = "on"
			}
			fmt.Printf("capture for stage %q is %s\n", s.ID, state)
			return nil
		},
	}
}

func newStagePromptCmd(client func() *Client) *cobra.Command {
	var file string
	var clear bool
	cmd := &cobra.Command{
		Use:   "prompt STAGE [TEXT]",
		Short: "Set or clear a stage's system-prompt override",
		Long: "Set the prompt from TEXT, from a file with --file, or clear it " +
			"with --clear. When set, the proxy substitutes this prompt for the " +
			"leading instruction message on every call tagged with the stage.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var prompt string
			switch {
			case clear:
				if file != "" || len(args) > 1 {
					return fmt.Errorf("--clear takes no prompt text")
				}
			case file != "":
				if len(args) > 1 {
					return fmt.Errorf("pass prompt text or --file, not both")
				}
				b, err := os.ReadFile(file) //nolint:gosec // the path is an operator-supplied CLI flag
				if err != nil {
					return fmt.Errorf("read prompt file: %w", err)
				}
				prompt = string(b)
			case len(args) == 2:
				prompt = args[1]
			default:
				return fmt.Errorf("provide prompt text, --file, or --clear")
			}
			s, err := client().PatchStage(cmd.Context(), args[0], wire.PatchStageRequest{Prompt: &prompt})
			if err != nil {
				return err
			}
			if s.Prompt == "" {
				fmt.Printf("cleared prompt for stage %q\n", s.ID)
			} else {
				fmt.Printf("set prompt for stage %q (%d chars)\n", s.ID, len(s.Prompt))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read the prompt from a file")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the stage's prompt")
	return cmd
}

func newStageMapCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "map STAGE BACKEND",
		Short: "Point a stage at a backend",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := client().MapStage(cmd.Context(), args[0], wire.MapStageRequest{Backend: args[1]})
			if err != nil {
				return err
			}
			fmt.Printf("mapped stage %q -> %s\n", s.ID, s.Backend)
			return nil
		},
	}
}

func newStageListCmd(client func() *Client) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List stage mappings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ss, err := client().ListStages(cmd.Context())
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(ss)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "STAGE\tBACKEND\tPROMPT\tCAPTURE")
			for _, s := range ss {
				prompt := "-"
				if s.Prompt != "" {
					prompt = "set"
				}
				capture := "off"
				if s.CaptureIO {
					capture = "on"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.ID, cmp.Or(s.Backend, "-"), prompt, capture)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table or json")
	return cmd
}

func newStageGetCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "get STAGE",
		Short: "Show one stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := client().GetStage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(s)
		},
	}
}

func newStageRmCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "rm STAGE",
		Short: "Remove a stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := client().DeleteStage(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("removed stage %q\n", args[0])
			return nil
		},
	}
}

func newMappingCmd(client func() *Client) *cobra.Command {
	cmd := &cobra.Command{Use: "mapping", Short: "Manage mapping variants"}
	cmd.AddCommand(
		newMappingSetCmd(client),
		newMappingListCmd(client),
		newMappingGetCmd(client),
		newMappingRmCmd(client),
	)
	return cmd
}

func newMappingSetCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "set NAME STAGE=BACKEND [STAGE=BACKEND ...]",
		Short: "Create or replace a mapping variant",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides := make(map[string]string, len(args)-1)
			for _, pair := range args[1:] {
				stageID, backend, ok := strings.Cut(pair, "=")
				if !ok || stageID == "" || backend == "" {
					return fmt.Errorf("override %q must be STAGE=BACKEND", pair)
				}
				overrides[stageID] = backend
			}
			v, err := client().PutMapping(cmd.Context(), wire.PutMappingRequest{
				Name:      args[0],
				Overrides: overrides,
			})
			if err != nil {
				return err
			}
			fmt.Printf("set mapping %q with %d override(s)\n", v.Name, len(v.Overrides))
			return nil
		},
	}
}

func newMappingListCmd(client func() *Client) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List mapping variants",
		RunE: func(cmd *cobra.Command, _ []string) error {
			vs, err := client().ListMappings(cmd.Context())
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(vs)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "MAPPING\tOVERRIDES")
			for _, v := range vs {
				parts := make([]string, 0, len(v.Overrides))
				for _, stageID := range slices.Sorted(maps.Keys(v.Overrides)) {
					parts = append(parts, stageID+"="+v.Overrides[stageID])
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\n", v.Name, strings.Join(parts, " "))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table or json")
	return cmd
}

func newMappingGetCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "get NAME",
		Short: "Show one mapping variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := client().GetMapping(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(v)
		},
	}
}

func newMappingRmCmd(client func() *Client) *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Remove a mapping variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := client().DeleteMapping(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("removed mapping %q\n", args[0])
			return nil
		},
	}
}

func newFeedbackCmd(client func() *Client) *cobra.Command {
	var stage, note string
	var rating float64
	cmd := &cobra.Command{
		Use:   "feedback COMPLETION_ID",
		Short: "Report the outcome of a completion for a stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := wire.FeedbackRequest{CompletionID: args[0], StageID: stage, Notes: note}
			if cmd.Flags().Changed("rating") {
				req.Rating = &rating
			}
			if err := client().SubmitFeedback(cmd.Context(), req); err != nil {
				return err
			}
			fmt.Printf("recorded feedback for %s (stage %q)\n", args[0], stage)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&stage, "stage", "", "stage the completion belongs to (required)")
	f.Float64Var(&rating, "rating", 0, "rating between 0 and 1")
	f.StringVar(&note, "note", "", "optional free-text note")
	_ = cmd.MarkFlagRequired("stage")
	return cmd
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func ptr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
