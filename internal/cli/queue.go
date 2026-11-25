package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

func registerQueueCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newQueueWorkCommand(state),
		newQueueStatsCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newQueueWorkCommand(state *State) *cobra.Command {
	var queuesFlag string
	var maxWorkers int
	var once bool
	var onceTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "queue:work",
		Short: "Start a queue worker (River)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if state.Config().Queue.Driver != "river" {
				return fmt.Errorf("queue:work requires QUEUE_DRIVER=river (sync executes jobs inline)")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				cfg := platform.Config
				opts := &queue.RiverOptions{Concurrency: maxWorkers}
				if queuesFlag != "" {
					for _, name := range strings.Split(queuesFlag, ",") {
						name = strings.TrimSpace(name)
						if name == "" {
							continue
						}
						opts.Queues = append(opts.Queues, queue.Name(name))
					}
				}

				engine, err := queue.NewRiverEngine(platform.DB.Primary().Pool, platform.QueueRegistry, cfg.Queue, state.Logger(), opts)
				if err != nil {
					return err
				}
				if platform.Telemetry != nil {
					engine.SetObserver(platform.Telemetry)
				}

				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()

				if once {
					fmt.Fprintln(cmd.OutOrStdout(), "Processing due jobs (once)...")
					return engine.Once(ctx, onceTimeout)
				}

				fmt.Fprintln(cmd.OutOrStdout(), "Queue worker started. Press Ctrl-C to stop.")
				if err := engine.Start(ctx); err != nil {
					return err
				}
				stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				if err := engine.Stop(stopCtx); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Queue worker stopped.")
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&queuesFlag, "queues", "", "comma-separated queues to process (default: critical,default,heavy)")
	cmd.Flags().IntVar(&maxWorkers, "max-workers", 0, "max workers per queue (default: QUEUE_WORKER_CONCURRENCY)")
	cmd.Flags().BoolVar(&once, "once", false, "process due jobs and exit")
	cmd.Flags().DurationVar(&onceTimeout, "once-timeout", 30*time.Second, "how long --once waits for a job")
	return cmd
}

func newQueueStatsCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "queue:stats",
		Short: "Show queue depths and failed counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				stats, err := queue.Stats(cmd.Context(), platform.DB.Primary().Pool)
				if err != nil {
					return err
				}

				writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(writer, "QUEUE\tTOTAL\tACTIVE\tFAILED")
				if len(stats) == 0 {
					fmt.Fprintln(writer, "(none)\t0\t0\t0")
				}
				for _, stat := range stats {
					fmt.Fprintf(writer, "%s\t%d\t%d\t%d\n", stat.Queue, stat.Total, stat.Active, stat.Failed)
				}
				return writer.Flush()
			})
		},
	}
}
