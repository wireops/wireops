package handlers

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/executor"
	"github.com/wireops/wireops/worker/metrics"
)

var (
	ActiveCommands       sync.Map // commandID -> context.CancelFunc
	ActiveJobs           sync.Map // jobRunID -> struct{}
	HeavySemaphore       chan struct{}
	LightSemaphore       chan struct{}
	InteractiveSemaphore chan struct{}
	MaxQueueDepth        int
	acceptingWork        atomic.Bool

	// completedDurableCommands caches the final CommandResult of durable
	// (deploy/redeploy/teardown) commands for a while so a redelivered copy
	// of an already-finished command (e.g. resent after the server missed
	// the original ack/result due to a reconnect) gets the cached outcome
	// echoed back instead of re-running `docker compose` a second time.
	completedDurableCommands  sync.Map // commandID -> durableResultEntry
	completedDurableMu        sync.Mutex
	completedDurableRetention = 30 * time.Minute
)

type durableResultEntry struct {
	result protocol.CommandResult
	at     time.Time
}

// ackDurableCommand sends the transport-level receipt ack for a durable
// command (if it carries a MessageID) before execution starts, so the
// server can tell "worker got it" apart from "worker finished it".
func ackDurableCommand(sender Sender, messageID string) {
	if messageID == "" {
		return
	}
	sender.SendEnvelope(protocol.Envelope{
		Type:    protocol.MsgAck,
		Payload: protocol.AckMessage{MessageID: messageID},
	})
}

// beginDurableCommand acks receipt of a durable command and reports whether
// the caller should proceed with execution. It returns false when the same
// CommandID is already running or has already finished, in which case it
// re-sends the appropriate response itself so a redelivered command never
// re-executes a destructive operation twice.
func beginDurableCommand(sender Sender, commandID, messageID string) bool {
	ackDurableCommand(sender, messageID)

	if _, running := ActiveCommands.Load(commandID); running {
		log.Printf("[worker] ignoring duplicate delivery of in-flight command %s", commandID)
		return false
	}

	if cached, ok := completedDurableCommands.Load(commandID); ok {
		entry := cached.(durableResultEntry)
		if time.Since(entry.at) <= completedDurableRetention {
			log.Printf("[worker] replaying cached result for duplicate command %s", commandID)
			sender.SendResult(entry.result)
			return false
		}
		completedDurableCommands.Delete(commandID)
	}

	return true
}

// finishDurableCommand caches the result of a durable command so a later
// duplicate delivery can be answered without re-executing, and opportunistically
// sweeps expired cache entries.
func finishDurableCommand(commandID string, result protocol.CommandResult) {
	completedDurableCommands.Store(commandID, durableResultEntry{result: result, at: time.Now()})

	if completedDurableMu.TryLock() {
		defer completedDurableMu.Unlock()
		now := time.Now()
		completedDurableCommands.Range(func(k, v any) bool {
			if now.Sub(v.(durableResultEntry).at) > completedDurableRetention {
				completedDurableCommands.Delete(k)
			}
			return true
		})
	}
}

type Sender interface {
	SendResult(res protocol.CommandResult)
	SendEnvelope(env protocol.Envelope)
	ReportJobCompleted(msg protocol.JobCompletedMessage)
	QueuedEnvelopesLen() int
	QueuedJobsLen() int
}

func InitSemaphores(heavy, light, interactive, queueDepth int) {
	if heavy <= 0 {
		panic("InitSemaphores: heavy parameter must be greater than zero")
	}
	if light <= 0 {
		panic("InitSemaphores: light parameter must be greater than zero")
	}
	if interactive <= 0 {
		panic("InitSemaphores: interactive parameter must be greater than zero")
	}
	HeavySemaphore = make(chan struct{}, heavy)
	LightSemaphore = make(chan struct{}, light)
	InteractiveSemaphore = make(chan struct{}, interactive)
	MaxQueueDepth = queueDepth
	acceptingWork.Store(true)
}

func SetAcceptingWork(accept bool) {
	acceptingWork.Store(accept)
}

func IsAcceptingWork() bool {
	return acceptingWork.Load()
}

func extractCommandID(payload interface{}) string {
	if m, ok := payload.(map[string]interface{}); ok {
		if cid, ok := m["command_id"].(string); ok {
			return cid
		}
	}
	return ""
}

func unmarshalPayload[T any](payload interface{}) (T, error) {
	var zero T
	b, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func unmarshalPayloadOrReply[T any](sender Sender, payload interface{}, defaultCmdID string) (T, bool) {
	cmd, err := unmarshalPayload[T](payload)
	if err != nil {
		cmdID := defaultCmdID
		if cmdID == "" {
			cmdID = extractCommandID(payload)
		}
		if cmdID != "" {
			log.Printf("[worker] invalid payload error=%v, replying with error", err)
			sender.SendResult(protocol.CommandResult{
				CommandID: cmdID,
				Error:     "invalid payload: " + err.Error(),
			})
		} else {
			log.Printf("[worker] invalid payload error=%v, unable to reply (missing command_id)", err)
		}
		return cmd, false
	}
	return cmd, true
}

func HandleDeploy(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.DeployCommand](sender, payload, "")
	if !ok {
		return
	}
	if !beginDurableCommand(sender, cmd.CommandID, cmd.MessageID) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Deploy(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metrics.TasksDeploy, 1)
	atomic.AddUint64(&metrics.TasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metrics.TasksError, 1)
	} else {
		atomic.AddUint64(&metrics.TasksSuccess, 1)
	}

	finishDurableCommand(cmd.CommandID, result)
	sender.SendResult(result)
}

func HandleRedeploy(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.RedeployCommand](sender, payload, "")
	if !ok {
		return
	}
	commandID := cmd.DeployCommand.CommandID
	if !beginDurableCommand(sender, commandID, cmd.MessageID) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(commandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(commandID)
	}()
	start := time.Now()
	result := executor.Redeploy(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metrics.TasksRedeploy, 1)
	atomic.AddUint64(&metrics.TasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metrics.TasksError, 1)
	} else {
		atomic.AddUint64(&metrics.TasksSuccess, 1)
	}

	finishDurableCommand(commandID, result)
	sender.SendResult(result)
}

func HandleTeardown(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.TeardownCommand](sender, payload, "")
	if !ok {
		return
	}
	if !beginDurableCommand(sender, cmd.CommandID, cmd.MessageID) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Teardown(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metrics.TasksTeardown, 1)
	atomic.AddUint64(&metrics.TasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metrics.TasksError, 1)
	} else {
		atomic.AddUint64(&metrics.TasksSuccess, 1)
	}

	finishDurableCommand(cmd.CommandID, result)
	sender.SendResult(result)
}

func HandleProbe(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.ProbeCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Probe(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metrics.TasksProbe, 1)
	atomic.AddUint64(&metrics.TasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metrics.TasksError, 1)
	} else {
		atomic.AddUint64(&metrics.TasksSuccess, 1)
	}

	sender.SendResult(result)
}

func HandleInspect(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.InspectCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Inspect(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metrics.TasksInspect, 1)
	atomic.AddUint64(&metrics.TasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metrics.TasksError, 1)
	} else {
		atomic.AddUint64(&metrics.TasksSuccess, 1)
	}

	sender.SendResult(result)
}

func HandleGetResources(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.GetResourcesCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.GetResources(ctx, cmd)
	sender.SendResult(result)
}

func HandleGetStatus(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.GetStatusCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.GetStatus(ctx, cmd)
	sender.SendResult(result)
}

func HandleStopContainer(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.ContainerActionCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.StopContainer(ctx, cmd)
	sender.SendResult(result)
}

func HandleRestartContainer(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.ContainerActionCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.RestartContainer(ctx, cmd)
	sender.SendResult(result)
}

func HandleGetContainerStats(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.GetContainerStatsCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.GetContainerStats(ctx, cmd)
	sender.SendResult(result)
}

func HandleGetContainerLogs(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.GetContainerLogsCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.GetContainerLogs(ctx, cmd)
	sender.SendResult(result)
}

func HandleDiscoverProjects(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.DiscoverProjectsCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.DiscoverProjects(ctx, cmd)
	sender.SendResult(result)
}

func HandleReadFile(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.ReadFileCommand](sender, payload, "")
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ActiveCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		ActiveCommands.Delete(cmd.CommandID)
	}()
	result := executor.ReadFile(ctx, cmd)
	sender.SendResult(result)
}

func HandleRunJob(sender Sender, payload interface{}) {
	if !IsAcceptingWork() {
		cmdID := extractCommandID(payload)
		if cmdID != "" {
			sender.SendResult(protocol.CommandResult{
				CommandID: cmdID,
				Error:     "rejected: worker draining",
			})
		}
		return
	}

	cmd, ok := unmarshalPayloadOrReply[protocol.RunJobCommand](sender, payload, "")
	if !ok {
		return
	}

	ackDurableCommand(sender, cmd.MessageID)

	if cached, ok := completedDurableCommands.Load(cmd.CommandID); ok {
		entry := cached.(durableResultEntry)
		if time.Since(entry.at) <= completedDurableRetention {
			log.Printf("[worker] replaying cached accept result for duplicate run_job command %s", cmd.CommandID)
			sender.SendResult(entry.result)
			return
		}
		completedDurableCommands.Delete(cmd.CommandID)
	}

	if _, running := ActiveJobs.Load(cmd.JobRunID); running {
		// Job container is already running: this is a redelivered copy of a
		// command still in flight (e.g. resent after a reconnect). Re-ack the
		// original "queued" acceptance instead of starting a second container.
		log.Printf("[worker] job %s already running, re-acking duplicate run_job command %s", cmd.JobRunID, cmd.CommandID)
		sender.SendResult(protocol.CommandResult{CommandID: cmd.CommandID, Output: "queued"})
		return
	}

	receivedAt := time.Now()

	accepted := TryScheduleThrottled(HeavySemaphore, protocol.MsgRunJob, func() {
		ActiveJobs.Store(cmd.JobRunID, struct{}{})
		defer ActiveJobs.Delete(cmd.JobRunID)

		ctx, cancel := context.WithCancel(context.Background())
		ActiveCommands.Store(cmd.CommandID, cancel)
		defer func() {
			cancel()
			ActiveCommands.Delete(cmd.CommandID)
		}()

		startedAt := time.Now()
		// Call executor.RunJob synchronously. It blocks until the container completes.
		msg := executor.RunJob(ctx, cmd)
		finishedAt := time.Now()
		duration := time.Since(startedAt)

		atomic.AddUint64(&metrics.JobsTotal, 1)
		atomic.AddUint64(&metrics.JobsDurationSumNs, uint64(duration.Nanoseconds()))
		if msg.Success {
			atomic.AddUint64(&metrics.JobsSuccess, 1)
		} else {
			atomic.AddUint64(&metrics.JobsError, 1)
		}

		queueTime := startedAt.UnixMilli() - receivedAt.UnixMilli()
		if queueTime < 0 {
			queueTime = 0
		}
		msg.QueueTimeMs = queueTime
		msg.ExecutionTimeMs = finishedAt.UnixMilli() - startedAt.UnixMilli()

		// Send completion report
		sender.ReportJobCompleted(msg)
	})
	if !accepted {
		atomic.AddUint64(&metrics.OverloadRejectsTotal, 1)
		sender.SendResult(protocol.CommandResult{
			CommandID: cmd.CommandID,
			Error:     "rejected: worker overloaded",
		})
		return
	}

	// Immediate acknowledgment that the job was accepted and queued.
	acceptResult := protocol.CommandResult{CommandID: cmd.CommandID, Output: "queued"}
	finishDurableCommand(cmd.CommandID, acceptResult)
	sender.SendResult(acceptResult)
}

func HandleKillJob(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.KillJobCommand](sender, payload, "")
	if !ok {
		return
	}
	result := executor.KillJob(cmd)
	sender.SendResult(result)
}

func HandleCancelCommand(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.CancelCommand](sender, payload, "")
	if !ok {
		return
	}
	if cancel, ok := ActiveCommands.Load(cmd.TargetCommandID); ok {
		log.Printf("[worker] cancelling command: %s", cmd.TargetCommandID)
		cancel.(context.CancelFunc)()
	} else {
		log.Printf("[worker] command %s not running or already finished", cmd.TargetCommandID)
	}
}

func HandleGetMetrics(sender Sender, payload interface{}) {
	cmd, ok := unmarshalPayloadOrReply[protocol.GetMetricsCommand](sender, payload, "")
	if !ok {
		return
	}

	activeTasksCount := 0
	ActiveCommands.Range(func(_, _ any) bool {
		activeTasksCount++
		return true
	})

	activeJobsCount := 0
	ActiveJobs.Range(func(_, _ any) bool {
		activeJobsCount++
		return true
	})

	resPayload := protocol.GetMetricsResult{
		CommandID: cmd.CommandID,
		Metrics:   metrics.Serialize(cap(HeavySemaphore), activeTasksCount, activeJobsCount, sender.QueuedEnvelopesLen(), sender.QueuedJobsLen()),
	}

	sender.SendEnvelope(protocol.Envelope{
		Type:    protocol.MsgGetMetricsResult,
		Payload: resPayload,
	})
}

func IsQueueFull() bool {
	return atomic.LoadInt64(&metrics.QueuedTasks) >= int64(MaxQueueDepth)
}

func RunThrottled(sem chan struct{}, msgType protocol.MessageType, fn func()) {
	sem <- struct{}{}
	defer func() { <-sem }()
	fn()
}

func TryScheduleThrottled(sem chan struct{}, msgType protocol.MessageType, fn func()) bool {
	select {
	case sem <- struct{}{}:
		go func() {
			defer func() { <-sem }()
			fn()
		}()
		return true
	default:
	}

	if IsQueueFull() {
		return false
	}

	atomic.AddInt64(&metrics.QueuedTasks, 1)
	if atomic.LoadInt64(&metrics.QueuedTasks) > int64(MaxQueueDepth) {
		atomic.AddInt64(&metrics.QueuedTasks, -1)
		return false
	}

	go func() {
		log.Printf("[worker] task %s queued due to concurrency limits", msgType)
		RunThrottled(sem, msgType, func() {
			atomic.AddInt64(&metrics.QueuedTasks, -1)
			fn()
		})
	}()
	return true
}

func DispatchThrottled(sender Sender, sem chan struct{}, msgType protocol.MessageType, payload interface{}, handler func(Sender, interface{})) {
	if !IsAcceptingWork() {
		cmdID := extractCommandID(payload)
		if cmdID != "" {
			log.Printf("[worker] rejecting command %s while draining", cmdID)
			sender.SendResult(protocol.CommandResult{
				CommandID: cmdID,
				Error:     "rejected: worker draining",
			})
		}
		return
	}

	if TryScheduleThrottled(sem, msgType, func() { handler(sender, payload) }) {
		return
	}

	cmdID := extractCommandID(payload)
	if cmdID != "" {
		log.Printf("[worker] queue depth exceeded, rejecting command %s", cmdID)
		atomic.AddUint64(&metrics.OverloadRejectsTotal, 1)
		sender.SendResult(protocol.CommandResult{
			CommandID: cmdID,
			Error:     "rejected: worker overloaded",
		})
	}
}

func GetActiveJobsList() []string {
	var activeIDs []string
	ActiveJobs.Range(func(k, _ any) bool {
		activeIDs = append(activeIDs, k.(string))
		return true
	})
	return activeIDs
}

func GetActiveCommandsCount() int {
	count := 0
	ActiveCommands.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func GetActiveJobsCount() int {
	count := 0
	ActiveJobs.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
