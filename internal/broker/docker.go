package broker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

const maxDockerOutputBytes = 1 << 20

type DockerBackend struct {
	client *client.Client
}

func NewDockerBackend() (*DockerBackend, error) {
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("initialize Docker client: %w", err)
	}
	return &DockerBackend{client: dockerClient}, nil
}

func (b *DockerBackend) Close() error { return b.client.Close() }

func (b *DockerBackend) Status(ctx context.Context, container string) (string, error) {
	inspect, err := b.client.ContainerInspect(ctx, container, client.ContainerInspectOptions{})
	if err != nil {
		return "", mapDockerError(err)
	}
	return string(inspect.Container.State.Status), nil
}

func (b *DockerBackend) Logs(ctx context.Context, container string) (string, error) {
	reader, err := b.client.ContainerLogs(ctx, container, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "200",
	})
	if err != nil {
		return "", mapDockerError(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maxDockerOutputBytes+1))
	if err != nil {
		return "", fmt.Errorf("read container logs: %w", err)
	}
	if len(content) > maxDockerOutputBytes {
		return "", fmt.Errorf("container logs exceed size limit")
	}
	return string(content), nil
}

func (b *DockerBackend) Start(ctx context.Context, container string) error {
	_, err := b.client.ContainerStart(ctx, container, client.ContainerStartOptions{})
	return mapDockerError(err)
}

func (b *DockerBackend) Stop(ctx context.Context, container string) error {
	timeout := 10
	_, err := b.client.ContainerStop(ctx, container, client.ContainerStopOptions{Timeout: &timeout})
	return mapDockerError(err)
}

func (b *DockerBackend) Backup(ctx context.Context, container string) (int, string, error) {
	created, err := b.client.ExecCreate(ctx, container, client.ExecCreateOptions{
		Cmd:          []string{"backup"},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return -1, "", mapDockerError(err)
	}
	attached, err := b.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return -1, "", mapDockerError(err)
	}
	defer attached.Close()
	var output bytes.Buffer
	if _, err := io.Copy(&output, io.LimitReader(attached.Reader, maxDockerOutputBytes+1)); err != nil {
		return -1, output.String(), fmt.Errorf("read backup output: %w", err)
	}
	if output.Len() > maxDockerOutputBytes {
		return -1, "", fmt.Errorf("backup output exceeds size limit")
	}
	inspected, err := b.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return -1, output.String(), mapDockerError(err)
	}
	return inspected.ExitCode, output.String(), nil
}

func mapDockerError(err error) error {
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return errors.Join(ErrNotFound, err)
	}
	return err
}
