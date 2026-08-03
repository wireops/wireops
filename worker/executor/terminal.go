package executor

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/wireops/wireops/internal/docker"
)

// dockerSocketPaths are the well-known host paths for the Docker daemon's
// Unix socket — same paths internal/policy blocks at deploy time for
// docker_socket policy.
var dockerSocketPaths = map[string]bool{
	"/var/run/docker.sock": true,
	"/run/docker.sock":     true,
}

// mountExposesDockerSocket reports whether src, as a bind mount source,
// grants access to the Docker daemon socket — either directly or by mounting
// an ancestor directory that contains it (e.g. "/var/run"). Mirrors
// internal/policy.ValidateDockerSocket's ancestor-prefix matching so a
// terminal session can't reach the socket via a mount deploy-time policy
// would have blocked as too broad.
func mountExposesDockerSocket(src string) bool {
	src = filepath.Clean(strings.TrimSpace(src))
	prefix := src
	if prefix != string(filepath.Separator) {
		prefix += string(filepath.Separator)
	}
	for sockPath := range dockerSocketPaths {
		if src == sockPath || strings.HasPrefix(sockPath, prefix) {
			return true
		}
	}
	return false
}

// checkTerminalExecAllowed refuses to open a session inside a container that
// already has some form of host escape (privileged, host networking, or a
// mounted Docker socket). Unlike internal/policy's deploy-time checks, this
// is not configurable per worker policy — exec-ing into such a container
// hands out root on the host, which defeats the entire "scoped to one
// container" premise of the terminal feature, regardless of whether the
// operator has otherwise opted out of the privileged/host-network policy
// for deploys.
func checkTerminalExecAllowed(ctx context.Context, cli *docker.Client, containerID string) error {
	info, err := cli.Raw().ContainerInspect(ctx, containerID)
	if err != nil {
		return err
	}
	if info.HostConfig == nil {
		return nil
	}
	if info.HostConfig.Privileged {
		return errors.New("container runs privileged — terminal exec is blocked for privileged containers")
	}
	if info.HostConfig.NetworkMode.IsHost() {
		return errors.New("container uses host networking — terminal exec is blocked for host-network containers")
	}
	for _, m := range info.Mounts {
		if mountExposesDockerSocket(m.Source) {
			return errors.New("container has the Docker socket mounted — terminal exec is blocked for containers with host Docker access")
		}
	}
	return nil
}

// terminalSession tracks one open docker-exec session so later
// input/resize/close commands (delivered as separate WebSocket envelopes)
// can find it by SessionID.
type terminalSession struct {
	execID string
	hj     types.HijackedResponse
	cancel context.CancelFunc
	// cli is the Docker client used to create this session, reused by
	// ResizeTerminal instead of dialing a fresh one per resize — a resize
	// fires repeatedly during a browser window/pane drag, so that would mean
	// a new daemon connection (with its own connect + ping round trip) per
	// event. Closed once, in OpenTerminal's cleanup goroutine.
	cli *docker.Client
}

var terminalSessions sync.Map // sessionID (string) -> *terminalSession

// OpenTerminal starts an interactive docker-exec session (TTY attached)
// inside a container already verified to belong to projectName, and streams
// its output to onOutput until the process exits or CloseTerminal is called.
// onOutput and onClosed run on the goroutine this function spawns.
func OpenTerminal(sessionID, containerID, projectName string, shell []string, rows, cols uint, onOutput func(data []byte), onClosed func(exitCode int, errMsg string)) {
	if len(shell) == 0 {
		shell = []string{"/bin/sh"}
	}

	cli, err := verifyContainerAndGetClient(context.Background(), containerID, projectName)
	if err != nil {
		onClosed(-1, err.Error())
		return
	}

	if err := checkTerminalExecAllowed(context.Background(), cli, containerID); err != nil {
		cli.Close()
		onClosed(-1, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	execResp, err := cli.Raw().ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          shell,
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		cli.Close()
		cancel()
		onClosed(-1, err.Error())
		return
	}

	hj, err := cli.Raw().ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		cli.Close()
		cancel()
		onClosed(-1, err.Error())
		return
	}

	if rows > 0 && cols > 0 {
		_ = cli.Raw().ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{Height: rows, Width: cols})
	}

	sess := &terminalSession{execID: execResp.ID, hj: hj, cancel: cancel, cli: cli}
	terminalSessions.Store(sessionID, sess)

	go func() {
		defer func() {
			hj.Close()
			terminalSessions.Delete(sessionID)
			cancel()

			exitCode := -1
			if info, err := cli.Raw().ContainerExecInspect(context.Background(), execResp.ID); err == nil {
				exitCode = info.ExitCode
			}
			cli.Close()
			onClosed(exitCode, "")
		}()

		buf := make([]byte, 32*1024)
		for {
			n, readErr := hj.Reader.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				onOutput(chunk)
			}
			if readErr != nil {
				if ctx.Err() == nil {
					log.Printf("[worker] terminal session %s read ended: %v", sessionID, readErr)
				}
				return
			}
		}
	}()
}

// WriteTerminal forwards keystroke bytes to an open session's stdin. It is a
// no-op if the session is unknown (already closed, or a stale delivery).
func WriteTerminal(sessionID string, data []byte) {
	v, ok := terminalSessions.Load(sessionID)
	if !ok {
		return
	}
	sess := v.(*terminalSession)
	if _, err := sess.hj.Conn.Write(data); err != nil {
		log.Printf("[worker] terminal session %s write failed: %v", sessionID, err)
	}
}

// ResizeTerminal resizes an open session's pty, reusing the session's
// existing Docker client rather than dialing a new one. No-op if the
// session is unknown.
func ResizeTerminal(sessionID string, rows, cols uint) {
	v, ok := terminalSessions.Load(sessionID)
	if !ok {
		return
	}
	sess := v.(*terminalSession)
	_ = sess.cli.Raw().ContainerExecResize(context.Background(), sess.execID, container.ResizeOptions{Height: rows, Width: cols})
}

// CloseTerminal tears down an open session's exec process. Safe to call on
// an already-closed or unknown session.
func CloseTerminal(sessionID string) {
	v, ok := terminalSessions.Load(sessionID)
	if !ok {
		return
	}
	sess := v.(*terminalSession)
	sess.hj.Close()
	sess.cancel()
}
