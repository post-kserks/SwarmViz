package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/swarmviz/swarmviz/frontend"
	"github.com/swarmviz/swarmviz/pkg/core"
	"github.com/swarmviz/swarmviz/pkg/diff"
	"github.com/swarmviz/swarmviz/pkg/hub"
	"github.com/swarmviz/swarmviz/pkg/server"
	"github.com/swarmviz/swarmviz/pkg/stats"
	"github.com/swarmviz/swarmviz/pkg/watcher"
)

var cfg core.Config

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "swarmviz",
		Short:         "SwarmViz - Multi-Agent Swarm Visualizer",
		Long:          `SwarmViz v3.1 is a high-performance local multi-agent swarm visualizer written in Go.`,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Startup Fail-Fast Validation Sequence
			validator := core.NewValidator(&cfg)
			if err := validator.ValidateAll(); err != nil {
				return err
			}

			// 2. Initialize AgentEventHub Memory Store
			eventHub := hub.NewEventHub(hub.WithClaimTTL(cfg.ClaimTTL))

			// 3. Initialize Shadow Baseline Copier
			copierOpts := core.CopierOptions{
				RepoPath:    cfg.Path,
				MaxRepoSize: cfg.MaxRepoSizeBytes,
				PID:         os.Getpid(),
			}
			copier := core.NewShadowCopier(copierOpts)
			copyRes, err := copier.CreateBaseline()
			if err != nil {
				return err
			}

			cmd.Printf("Baseline shadow copy initialized at %s (%d files, %d bytes)\n",
				copyRes.ShadowDir, copyRes.CopiedCount, copyRes.TotalSizeBytes)

			// 4. Setup server instance with embedded frontend static assets
			staticFS, err := frontend.GetFS()
			if err != nil {
				cmd.Printf("⚠ Warning: failed to access embedded static assets: %v\n", err)
			}

			if !frontend.IsBuilt() {
				cmd.Printf("⚠ Warning: no UI is embedded in this binary. " +
					"Build it with `npm --prefix frontend ci && npm --prefix frontend run build`, " +
					"then rebuild; the API and WebSocket work regardless.\n")
			}

			// Workspace stats feed widget 3 (file tree) and widget 4 (LOC chart).
			// Seeded from the shadow copy so the tree lists exactly the tracked,
			// non-ignored files.
			statsTracker := stats.New(filepath.Base(cfg.Path), copyRes.CopiedFiles)

			serverInst := server.NewServer(
				eventHub,
				cfg.BufferSize,
				statsTracker,
				statsTracker,
				server.WithStaticFS(staticFS),
				server.WithAPIToken(cfg.APIToken),
			)

			// 5. Initialize Disk Usage Monitor & Degraded Flag
			var isDegradedAtomic int32
			monitorOpts := core.DiskMonitorOptions{
				ShadowDir:      copyRes.ShadowDir,
				MaxRepoSize:    cfg.MaxRepoSizeBytes,
				CheckInterval:  cfg.DiskCheckInterval,
				MaxFileLimitMB: 10,
				OnWarning: func(code, message string) {
					atomic.StoreInt32(&isDegradedAtomic, 1)
					cmd.Printf("⚠ SERVER_WARNING [%s]: %s\n", code, message)
					serverInst.BroadcastEvent("SERVER_WARNING", map[string]interface{}{
						"code":    code,
						"message": message,
					})
				},
			}
			diskMonitor := core.NewDiskMonitor(monitorOpts)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			diskMonitor.Start(ctx)

			// 6. Initialize Diff Engine
			diffEngine := diff.NewEngine(
				cfg.Path,
				copyRes.ShadowDir,
				eventHub,
				func() bool {
					return atomic.LoadInt32(&isDegradedAtomic) == 1
				},
			)

			// 7. Initialize and Start File Watcher
			watcherCfg := watcher.Config{
				RepoPath: cfg.Path,
				Interval: cfg.Interval,
			}
			fsWatcher, err := watcher.NewWatcher(watcherCfg)
			if err != nil {
				diskMonitor.Stop()
				_ = copier.Cleanup()
				return fmt.Errorf("failed to create watcher: %w", err)
			}

			if err := fsWatcher.Start(ctx); err != nil {
				diskMonitor.Stop()
				_ = copier.Cleanup()
				return fmt.Errorf("failed to start watcher: %w", err)
			}

			// 8. Event processing pipeline (Watcher -> DiffEngine -> WebSocket Server Broadcast)
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case ev, ok := <-fsWatcher.Events():
						if !ok {
							return
						}
						res, diffErr := diffEngine.ProcessEvent(ev)
						if diffErr != nil {
							cmd.Printf("Error processing diff event for %s: %v\n", ev.RelPath, diffErr)
							continue
						}
						if res != nil {
							serverInst.BroadcastEvent("LIVE_DIFF_STREAM", res)

							// Keep the file tree and LOC chart in step with the
							// stream instead of leaving them frozen at startup.
							editCount, totalAdded, totalRemoved := statsTracker.RecordEdit(res.File, res.Added, res.Removed)
							serverInst.BroadcastEvent("FILE_TREE_UPDATE", map[string]interface{}{
								"file":       res.File,
								"edit_count": editCount,
								"state":      string(res.Status),
							})
							serverInst.BroadcastEvent("LOC_DELTA_UPDATE", map[string]interface{}{
								"total_added":   totalAdded,
								"total_removed": totalRemoved,
							})
						}
					case watcherErr, ok := <-fsWatcher.Errors():
						if !ok {
							return
						}
						cmd.Printf("Watcher error: %v\n", watcherErr)
					}
				}
			}()

			// 9. Start HTTP Server
			addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
			httpServer := &http.Server{
				Addr:    addr,
				Handler: serverInst,
			}

			go func() {
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					cmd.Printf("HTTP server error: %v\n", err)
				}
			}()

			cmd.Printf("SwarmViz backend core initialized. Listening on %s:%d (Monitored Path: %s)\n",
				cfg.Host, cfg.Port, cfg.Path)

			// 10. Signal Trap & Graceful Shutdown with 3s Timeout
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			sig := <-sigChan
			cmd.Printf("Received signal %v, initiating shutdown...\n", sig)

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer shutdownCancel()

			done := make(chan struct{})
			go func() {
				cancel()
				_ = httpServer.Shutdown(shutdownCtx)
				fsWatcher.Stop()
				diskMonitor.Stop()
				diffEngine.Close()
				serverInst.Close()
				if err := copier.Cleanup(); err != nil {
					cmd.Printf("Error cleaning up shadow directory: %v\n", err)
				}
				close(done)
			}()

			select {
			case <-done:
				cmd.Println("Graceful shutdown completed successfully")
			case <-shutdownCtx.Done():
				cmd.Println("Graceful shutdown timed out (3s limit reached), forcing exit")
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().IntVarP(&cfg.Port, "port", "p", 8942, "Port for web interface")
	cmd.Flags().StringVarP(&cfg.Path, "path", "d", "./", "Path to monitored git repository")
	cmd.Flags().DurationVar(&cfg.Interval, "interval", 300*time.Millisecond, "Debounce interval for file events per file")
	cmd.Flags().DurationVar(&cfg.ClaimTTL, "claim-ttl", 2*time.Second, "TTL for agent file claims before auto-release")
	cmd.Flags().IntVar(&cfg.BufferSize, "buffer-size", 5000, "Size of WS event ring buffer for replay")
	cmd.Flags().StringVar(&cfg.Host, "host", "127.0.0.1", "Listen host address")
	cmd.Flags().StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	cmd.Flags().StringVar(&cfg.MaxRepoSizeStr, "max-repo-size", "500MB", "Repo size threshold for fail-fast startup & warning limit")
	cmd.Flags().DurationVar(&cfg.DiskCheckInterval, "disk-check-interval", 30*time.Second, "Frequency of periodic /tmp copy size re-check")
	cmd.Flags().StringVar(&cfg.APIToken, "api-token", "", "Require 'Authorization: Bearer <token>' on the ingest API (recommended with --host)")

	return cmd
}

func Execute() error {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		if err.Error() == "flag: help requested" {
			_ = cmd.Help()
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		return err
	}
	return nil
}
