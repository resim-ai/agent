package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"

	"github.com/docker/docker/api/types/container"
	api "github.com/resim-ai/agent/api"
	"github.com/stretchr/testify/mock"
)

const completeCheckinFields = `"workerImageURI": "public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538", "authToken": "foo-worker-token", "workerEnvironmentVariables": [["RERUN_WORKER_STUFF", "yes"]]`

// stubPauseCheckins replaces the mock API server with one that reports paused
// for the first pausedCheckins check-ins and unpaused thereafter. It records
// whether any check-in request carried the quiesced ack. When
// pausedResponseComplete is false the paused responses omit the worker fields,
// exercising that a paused check-in is handled before field validation.
func (s *AgentTestSuite) stubPauseCheckins(pausedCheckins int32, pausedResponseComplete bool) *atomic.Bool {
	sawAck := &atomic.Bool{}
	var count atomic.Int32

	s.mockAPIServer.Close()
	s.mockAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/checkin") {
			io.WriteString(w, "")
			return
		}
		var in api.AgentCheckinInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Paused != nil && *in.Paused {
			sawAck.Store(true)
		}
		if count.Add(1) <= pausedCheckins {
			if pausedResponseComplete {
				io.WriteString(w, `{`+completeCheckinFields+`, "paused": true}`)
			} else {
				io.WriteString(w, `{"paused": true}`)
			}
			return
		}
		io.WriteString(w, `{`+completeCheckinFields+`}`)
	}))
	os.Setenv("RESIM_AGENT_API_HOST", s.mockAPIServer.URL)
	s.agent.APIHost = s.mockAPIServer.URL
	return sawAck
}

// expectSuccessfulWorkerRun mocks one full worker container lifecycle.
func (s *AgentTestSuite) expectSuccessfulWorkerRun() {
	const containerID = "container-id"
	s.mockDocker.On("ImagePull", mock.Anything, mock.Anything, mock.Anything).
		Return(io.NopCloser(strings.NewReader("thing")), nil).Once()
	s.mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.MatchedBy(func(workerID string) bool { return strings.HasPrefix(workerID, "worker-") }),
	).Return(container.CreateResponse{ID: containerID}, nil).Once()
	s.mockDocker.On("ContainerStart", mock.Anything, containerID, mock.Anything).Return(nil).Once()
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(createTestContainer("running", true), nil).Once()
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(createTestContainer("succeeded", false), nil).Once()
	s.mockDocker.On("ContainerRemove", mock.Anything, containerID, mock.Anything).Return(nil).Once()
}

// TestStart_PausedThenUnpaused verifies a paused agent launches no worker while
// paused, self-reports the quiesced ack, and resumes on unpause. In one-task
// mode it stays alive through the pause and runs its single task afterwards.
func (s *AgentTestSuite) TestStart_PausedThenUnpaused() {
	s.agent.ConfigDirOverride = s.createConfigFile()
	s.Require().NoError(s.agent.LoadConfig())

	sawAck := s.stubPauseCheckins(2, true)
	s.expectSuccessfulWorkerRun()

	err := s.agent.Start()
	s.NoError(err)
	s.True(sawAck.Load(), "the agent must self-report the quiesced ack while paused")
	// The worker lifecycle mocks are asserted .Once() in TearDownTest, proving
	// exactly one worker launched — only after the unpause.
}

// TestStart_PausedDoesNotExhaustErrorBudget verifies that a long run of paused
// check-ins never trips the max-error process exit: with MaxErrorCount=1 the
// agent survives several paused (and deliberately field-incomplete) check-ins
// and still runs its task once unpaused.
func (s *AgentTestSuite) TestStart_PausedDoesNotExhaustErrorBudget() {
	s.agent.ConfigDirOverride = s.createConfigFile()
	s.Require().NoError(s.agent.LoadConfig())
	s.agent.MaxErrorCount = 1

	s.stubPauseCheckins(4, false)
	s.expectSuccessfulWorkerRun()

	err := s.agent.Start()
	s.NoError(err, "paused check-ins must not accumulate toward the max-error exit")
}
