package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	gosync "sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/deploymetrics"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/sync"
)

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

// PrometheusContentType is the Content-Type header for Prometheus text exposition.
const PrometheusContentType = prometheusContentType

const workerMetricsTimeout = 5 * time.Second

var prometheusLabelEscaper = strings.NewReplacer(
	"\\", `\\`,
	`"`, `\"`,
	"\n", `\n`,
)

// CollectWorker fetches Prometheus metrics from a single connected worker.
func CollectWorker(ctx context.Context, dispatcher sync.WorkerDispatcher, workerID string) (string, error) {
	if dispatcher == nil || !dispatcher.IsConnected(workerID) {
		return "", errors.New("worker offline")
	}

	cmdID := fmt.Sprintf("metrics-%s-%d", workerID, time.Now().UnixNano())
	res, err := dispatcher.Dispatch(ctx, workerID, protocol.GetMetricsCommand{
		CommandID: cmdID,
	})
	if err != nil {
		return "", err
	}
	return res.Output, nil
}

// CollectAll gathers and aggregates Prometheus metrics across all workers in parallel.
func CollectAll(ctx context.Context, app core.App, dispatcher sync.WorkerDispatcher) (string, error) {
	workers, err := app.FindAllRecords("workers")
	if err != nil {
		return "", err
	}

	type workerResult struct {
		workerID string
		hostname string
		metrics  string
		err      error
	}

	ch := make(chan workerResult, len(workers))
	var wg gosync.WaitGroup

	for _, workerRec := range workers {
		workerID := workerRec.Id
		hostname := workerRec.GetString("hostname")

		if dispatcher == nil || !dispatcher.IsConnected(workerID) {
			ch <- workerResult{
				workerID: workerID,
				hostname: hostname,
				err:      errors.New("offline"),
			}
			continue
		}

		wg.Add(1)
		go func(wID, hName string) {
			defer wg.Done()

			cmdID := fmt.Sprintf("metrics-%s-%d", wID, time.Now().UnixNano())
			reqCtx, cancel := context.WithTimeout(ctx, workerMetricsTimeout)
			defer cancel()

			res, dispatchErr := dispatcher.Dispatch(reqCtx, wID, protocol.GetMetricsCommand{
				CommandID: cmdID,
			})
			if dispatchErr != nil {
				ch <- workerResult{
					workerID: wID,
					hostname: hName,
					err:      dispatchErr,
				}
				return
			}

			ch <- workerResult{
				workerID: wID,
				hostname: hName,
				metrics:  res.Output,
			}
		}(workerID, hostname)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var sb strings.Builder
	writeAggregateHeaders(&sb)

	// Server-side (control-plane) deploy timeline metrics — not per-worker,
	// so they're written once here rather than per-connection like the
	// worker-sourced metrics below.
	sb.WriteString(deploymetrics.Serialize())

	for res := range ch {
		if res.err != nil {
			sb.WriteString(fmt.Sprintf(
				"wireops_worker_connected{worker=\"%s\",hostname=\"%s\"} 0\n",
				escapePrometheusLabelValue(res.workerID),
				escapePrometheusLabelValue(res.hostname),
			))
			continue
		}

		if res.metrics != "" {
			sb.WriteString(InjectWorkerLabels(res.metrics, res.workerID, res.hostname))
		}
	}

	return sb.String(), nil
}

func writeAggregateHeaders(sb *strings.Builder) {
	sb.WriteString("# HELP wireops_worker_connected WebSocket connection status\n")
	sb.WriteString("# TYPE wireops_worker_connected gauge\n")
	sb.WriteString("# HELP wireops_worker_connection_attempts_total Total registration/connection attempts\n")
	sb.WriteString("# TYPE wireops_worker_connection_attempts_total counter\n")
	sb.WriteString("# HELP wireops_worker_concurrency_limit Configured task concurrency limit\n")
	sb.WriteString("# TYPE wireops_worker_concurrency_limit gauge\n")
	sb.WriteString("# HELP wireops_worker_active_tasks Currently active task executions\n")
	sb.WriteString("# TYPE wireops_worker_active_tasks gauge\n")
	sb.WriteString("# HELP wireops_worker_queued_tasks Tasks currently waiting in the semaphore queue\n")
	sb.WriteString("# TYPE wireops_worker_queued_tasks gauge\n")
	sb.WriteString("# HELP wireops_worker_tasks_total Total stack tasks processed by type\n")
	sb.WriteString("# TYPE wireops_worker_tasks_total counter\n")
	sb.WriteString("# HELP wireops_worker_tasks_outcome_total Total stack tasks outcomes\n")
	sb.WriteString("# TYPE wireops_worker_tasks_outcome_total counter\n")
	sb.WriteString("# HELP wireops_worker_task_duration_seconds_sum Total time spent processing tasks in seconds\n")
	sb.WriteString("# TYPE wireops_worker_task_duration_seconds_sum counter\n")
	sb.WriteString("# HELP wireops_worker_task_duration_seconds_count Total number of tasks measured\n")
	sb.WriteString("# TYPE wireops_worker_task_duration_seconds_count counter\n")
	sb.WriteString("# HELP wireops_worker_jobs_total Total Docker jobs executed by outcome\n")
	sb.WriteString("# TYPE wireops_worker_jobs_total counter\n")
	sb.WriteString("# HELP wireops_worker_active_jobs Currently active Docker job runs\n")
	sb.WriteString("# TYPE wireops_worker_active_jobs gauge\n")
	sb.WriteString("# HELP wireops_worker_job_duration_seconds_sum Total time spent executing jobs in seconds\n")
	sb.WriteString("# TYPE wireops_worker_job_duration_seconds_sum counter\n")
	sb.WriteString("# HELP wireops_worker_job_duration_seconds_count Total number of Docker jobs measured\n")
	sb.WriteString("# TYPE wireops_worker_job_duration_seconds_count counter\n")
	sb.WriteString("# HELP wireops_worker_queued_messages Outbound messages buffered in memory\n")
	sb.WriteString("# TYPE wireops_worker_queued_messages gauge\n")
	sb.WriteString("# HELP wireops_worker_dropped_messages_total Total outbound messages dropped due to buffer limits\n")
	sb.WriteString("# TYPE wireops_worker_dropped_messages_total counter\n")
	sb.WriteString("# HELP wireops_worker_flush_attempts_total Total spool flush send attempts\n")
	sb.WriteString("# TYPE wireops_worker_flush_attempts_total counter\n")
	sb.WriteString("# HELP wireops_worker_flush_acked_total Total spool messages acknowledged by the server\n")
	sb.WriteString("# TYPE wireops_worker_flush_acked_total counter\n")
	sb.WriteString("# HELP wireops_worker_flush_failed_total Total spool flush failures\n")
	sb.WriteString("# TYPE wireops_worker_flush_failed_total counter\n")
	sb.WriteString("# HELP wireops_worker_overload_rejections_total Total commands rejected because the worker was overloaded\n")
	sb.WriteString("# TYPE wireops_worker_overload_rejections_total counter\n")
}

// InjectWorkerLabels adds worker and hostname labels to each metric sample line.
func InjectWorkerLabels(metricsText, workerID, hostname string) string {
	escapedWorkerID := escapePrometheusLabelValue(workerID)
	escapedHostname := escapePrometheusLabelValue(hostname)
	lines := strings.Split(metricsText, "\n")
	var sb strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		spaceIdx := strings.Index(trimmed, " ")
		if spaceIdx == -1 {
			continue
		}

		nameAndLabels := trimmed[:spaceIdx]
		value := trimmed[spaceIdx:]

		var rewritten string
		if strings.Contains(nameAndLabels, "{") {
			braceIdx := strings.LastIndex(nameAndLabels, "}")
			if braceIdx != -1 {
				rewritten = nameAndLabels[:braceIdx] + fmt.Sprintf(`,worker="%s",hostname="%s"`, escapedWorkerID, escapedHostname) + "}"
			} else {
				rewritten = nameAndLabels
			}
		} else {
			rewritten = nameAndLabels + fmt.Sprintf(`{worker="%s",hostname="%s"}`, escapedWorkerID, escapedHostname)
		}

		sb.WriteString(rewritten + value + "\n")
	}
	return sb.String()
}

func escapePrometheusLabelValue(value string) string {
	return prometheusLabelEscaper.Replace(value)
}
