package main

import (
	"context"
	"io"
	"iter"

	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/mock"
)

type DockerClient interface {
	ImagePull(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	Close() error
	ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

type MockDockerClient struct {
	mock.Mock
}

// mockImagePullResponse adapts a plain io.ReadCloser into the
// client.ImagePullResponse interface so mocks can keep returning readers.
type mockImagePullResponse struct {
	io.ReadCloser
}

func (mockImagePullResponse) JSONMessages(ctx context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(yield func(jsonstream.Message, error) bool) {}
}

func (mockImagePullResponse) Wait(ctx context.Context) error {
	return nil
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	args := m.Called(ctx, refStr, options)
	rc, _ := args.Get(0).(io.ReadCloser)
	return mockImagePullResponse{rc}, args.Error(1)
}

func (m *MockDockerClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	args := m.Called(ctx, options.Config, options.HostConfig, options.Platform, options.Name)
	return args.Get(0).(client.ContainerCreateResult), args.Error(1)
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	args := m.Called(ctx, containerID)
	return args.Get(0).(client.ContainerInspectResult), args.Error(1)
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerStartResult), args.Error(1)
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	args := m.Called(ctx, containerID, options)
	return args.Get(0).(client.ContainerRemoveResult), args.Error(1)
}
