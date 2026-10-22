package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/wireops/wireops/internal/protocol"
)

const (
	// execStreamLimit caps each captured stream. The output crosses the
	// WebSocket and then lands in a model's context, so an unbounded
	// `cat /var/log/everything` has to be cut somewhere; the result says it
	// was.
	execStreamLimit = 256 * 1024
	// execDefaultTimeout bounds a run the caller did not bound, and
	// execMaxTimeout what it may ask for. Without a deadline here a command
	// that never exits would hold an exec open on the host after the server
	// stopped waiting for it.
	execDefaultTimeout = 30 * time.Second
	execMaxTimeout     = 300 * time.Second
)

// ExecOneShot runs a single non-interactive command inside a container of a
// stack and reports its stdout, stderr and exit code.
//
// It exists because the alternative — driving the interactive pty session
// (OpenTerminal) and watching the transcript — can only ever approximate
// this: a pty merges stdout and stderr into one stream, echoes the input back
// into it, and gives no completion signal, so the caller has to append a
// marker command and therefore require a POSIX shell. Here the container
// runtime reports the exit status and the streams stay separate.
func ExecOneShot(ctx context.Context, cmd protocol.ExecCommand) protocol.CommandResult {
	log.Printf("[executor] exec start stack=%s project=%s container=%s command=%s argv_len=%d",
		cmd.StackID, cmd.ProjectName, cmd.ContainerID, cmd.CommandID, len(cmd.Cmd))

	if len(cmd.Cmd) == 0 {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "exec requires a non-empty cmd"}
	}

	cli, err := verifyContainerAndGetClient(ctx, cmd.ContainerID, cmd.ProjectName, cmd.StackID)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	// The same gate the interactive terminal uses: exec-ing into a
	// privileged, host-network or docker-socket container hands out the host,
	// which no per-worker policy may opt out of.
	if err := checkTerminalExecAllowed(ctx, cli, cmd.ContainerID); err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	timeout := execDefaultTimeout
	if cmd.TimeoutSeconds > 0 {
		timeout = time.Duration(cmd.TimeoutSeconds) * time.Second
		if timeout > execMaxTimeout {
			timeout = execMaxTimeout
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	created, err := cli.Raw().ContainerExecCreate(runCtx, cmd.ContainerID, container.ExecOptions{
		Cmd: cmd.Cmd,
		// No TTY: that is what keeps stdout and stderr apart on the wire, as
		// a multiplexed stream stdcopy can demultiplex below.
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   cmd.WorkingDir,
		User:         cmd.User,
		Env:          cmd.Env,
	})
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: fmt.Sprintf("creating exec: %v", err)}
	}

	attached, err := cli.Raw().ContainerExecAttach(runCtx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: fmt.Sprintf("attaching to exec: %v", err)}
	}
	defer attached.Close()
	// Once docker hijacks the connection it stops observing runCtx, so a
	// command that keeps the stream open (sleep 600) would leave StdCopy
	// blocked past the deadline — holding the interactive semaphore with it.
	// Closing the connection when runCtx ends is what unblocks the read.
	stopWatch := context.AfterFunc(runCtx, func() { attached.Close() })
	defer stopWatch()

	var stdout, stderr bytes.Buffer
	// stdcopy demultiplexes docker's framed stream into the two buffers. The
	// cap is per stream and drops the excess silently, so the command still
	// runs to completion — and its exit code stays meaningful — instead of
	// dying on a short write.
	outWriter := limitWriter(&stdout, execStreamLimit)
	errWriter := limitWriter(&stderr, execStreamLimit)
	_, copyErr := stdcopy.StdCopy(outWriter, errWriter, attached.Reader)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if copyErr != nil && !timedOut && !errors.Is(copyErr, io.EOF) {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: fmt.Sprintf("reading exec output: %v", copyErr)}
	}

	result := protocol.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: -1,
		TimedOut: timedOut,
		// Asked of the writer, not of the buffer length: output that is
		// exactly at the cap lost nothing and must not be reported as cut.
		StdoutTruncated: outWriter.dropped,
		StderrTruncated: errWriter.dropped,
	}

	if !timedOut {
		// Inspected on a fresh context: runCtx may be at its deadline, and
		// the exit code is the one thing worth one more call.
		inspectCtx, inspectCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer inspectCancel()
		if info, err := cli.Raw().ContainerExecInspect(inspectCtx, created.ID); err == nil {
			result.ExitCode = info.ExitCode
		}
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: fmt.Sprintf("encoding exec result: %v", err)}
	}

	log.Printf("[executor] exec done stack=%s container=%s command=%s exit=%d timed_out=%t stdout=%dB stderr=%dB",
		cmd.StackID, cmd.ContainerID, cmd.CommandID, result.ExitCode, result.TimedOut, len(result.Stdout), len(result.Stderr))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// limitWriter writes at most limit bytes to w and silently drops the rest, so
// a stream cap never turns into a short write that stdcopy would report as an
// error (and that would cut the command off before its exit status).
func limitWriter(w io.Writer, limit int) *cappedWriter {
	return &cappedWriter{w: w, remaining: limit}
}

type cappedWriter struct {
	w         io.Writer
	remaining int
	// dropped records that bytes were actually discarded, which is not the
	// same as having reached the cap: a stream of exactly limit bytes is
	// complete.
	dropped bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.remaining <= 0 {
		if len(p) > 0 {
			c.dropped = true
		}
		return len(p), nil
	}
	if len(p) > c.remaining {
		if _, err := c.w.Write(p[:c.remaining]); err != nil {
			return 0, err
		}
		c.remaining = 0
		c.dropped = true
		return len(p), nil
	}
	n, err := c.w.Write(p)
	c.remaining -= n
	return n, err
}
