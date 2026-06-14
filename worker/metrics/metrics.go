package metrics

import (
	"fmt"
	"strings"
	"sync/atomic"
)

var (
	ConnAttempts         uint64
	QueuedTasks        int64
	TasksDeploy        uint64
	TasksRedeploy      uint64
	TasksTeardown      uint64
	TasksProbe         uint64
	TasksInspect       uint64
	TasksSuccess       uint64
	TasksError         uint64
	TasksDurationSumNs uint64
	JobsTotal          uint64
	JobsSuccess        uint64
	JobsError          uint64
	JobsDurationSumNs  uint64
	DroppedMessagesTotal uint64
)

func Serialize(concurrencyLimit, activeTasksCount, activeJobsCount, qEnvLen, qJobsLen int) string {
	var sb strings.Builder

	writeMetric := func(name, help, mType string, value interface{}, labels ...string) {
		sb.WriteString("# HELP " + name + " " + help + "\n")
		sb.WriteString("# TYPE " + name + " " + mType + "\n")
		sb.WriteString(name)
		if len(labels) > 0 {
			sb.WriteString("{" + strings.Join(labels, ",") + "}")
		}
		sb.WriteString(" " + fmt.Sprintf("%v", value) + "\n")
	}

	// 1. Connection
	writeMetric("wireops_worker_connected", "WebSocket connection status", "gauge", 1)
	writeMetric("wireops_worker_connection_attempts_total", "Total registration/connection attempts", "counter", atomic.LoadUint64(&ConnAttempts))

	// 2. Concurrency
	writeMetric("wireops_worker_concurrency_limit", "Configured task concurrency limit", "gauge", concurrencyLimit)
	writeMetric("wireops_worker_active_tasks", "Currently active task executions", "gauge", activeTasksCount)
	writeMetric("wireops_worker_queued_tasks", "Tasks currently waiting in the semaphore queue", "gauge", atomic.LoadInt64(&QueuedTasks))

	// 3. Task Executions
	writeMetric("wireops_worker_tasks_total", "Total stack tasks processed by type", "counter", atomic.LoadUint64(&TasksDeploy), "type=\"deploy\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"redeploy\"} %d\n", atomic.LoadUint64(&TasksRedeploy)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"teardown\"} %d\n", atomic.LoadUint64(&TasksTeardown)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"probe\"} %d\n", atomic.LoadUint64(&TasksProbe)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"inspect\"} %d\n", atomic.LoadUint64(&TasksInspect)))

	writeMetric("wireops_worker_tasks_outcome_total", "Total stack tasks outcomes", "counter", atomic.LoadUint64(&TasksSuccess), "status=\"success\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_outcome_total{status=\"error\"} %d\n", atomic.LoadUint64(&TasksError)))

	writeMetric("wireops_worker_task_duration_seconds_sum", "Total time spent processing tasks in seconds", "counter", float64(atomic.LoadUint64(&TasksDurationSumNs))/1e9)
	writeMetric("wireops_worker_task_duration_seconds_count", "Total number of tasks measured", "counter", atomic.LoadUint64(&TasksSuccess)+atomic.LoadUint64(&TasksError))

	// 4. Job Executions
	writeMetric("wireops_worker_jobs_total", "Total Docker jobs executed by outcome", "counter", atomic.LoadUint64(&JobsSuccess), "status=\"success\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_jobs_total{status=\"error\"} %d\n", atomic.LoadUint64(&JobsError)))
	writeMetric("wireops_worker_active_jobs", "Currently active Docker job runs", "gauge", activeJobsCount)

	writeMetric("wireops_worker_job_duration_seconds_sum", "Total time spent executing jobs in seconds", "counter", float64(atomic.LoadUint64(&JobsDurationSumNs))/1e9)
	writeMetric("wireops_worker_job_duration_seconds_count", "Total number of Docker jobs measured", "counter", atomic.LoadUint64(&JobsSuccess)+atomic.LoadUint64(&JobsError))

	// 5. Queued Messages
	writeMetric("wireops_worker_queued_messages", "Outbound messages buffered in memory", "gauge", qEnvLen, "queue=\"results\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_queued_messages{queue=\"completed_jobs\"} %d\n", qJobsLen))
	writeMetric("wireops_worker_dropped_messages_total", "Total outbound messages dropped due to buffer limits", "counter", atomic.LoadUint64(&DroppedMessagesTotal))

	return sb.String()
}
