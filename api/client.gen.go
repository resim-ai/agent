// Package api provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.3.0 DO NOT EDIT.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oapi-codegen/runtime"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const (
	OAuthScopes = "OAuth.Scopes"
)

// Defines values for TaskStatus.
const (
	ERROR     TaskStatus = "ERROR"
	RUNNING   TaskStatus = "RUNNING"
	STARTING  TaskStatus = "STARTING"
	SUCCEEDED TaskStatus = "SUCCEEDED"
)

// Defines values for TaskType.
const (
	BATCHMETRICS TaskType = "BATCH_METRICS"
	EXPERIENCE   TaskType = "EXPERIENCE"
	METRICS      TaskType = "METRICS"
	REPORT       TaskType = "REPORT"
)

// AgentHeartbeatInput defines model for agentHeartbeatInput.
type AgentHeartbeatInput struct {
	AgentName  *string      `json:"agentName,omitempty"`
	PoolLabels *[]PoolLabel `json:"poolLabels,omitempty"`
	TaskName   *TaskName    `json:"taskName,omitempty"`
	TaskStatus *TaskStatus  `json:"taskStatus,omitempty"`
}

// PoolLabel defines model for poolLabel.
type PoolLabel = string

// TaskName defines model for taskName.
type TaskName = string

// TaskRecordCreationOutput defines model for taskRecordCreationOutput.
type TaskRecordCreationOutput = openapi_types.UUID

// TaskRecordInput defines model for taskRecordInput.
type TaskRecordInput struct {
	Billable       bool               `json:"billable"`
	EndTimestamp   time.Time          `json:"endTimestamp"`
	Gpu            int                `json:"gpu"`
	MemoryMiB      int                `json:"memoryMiB"`
	OrgID          string             `json:"orgID"`
	ParentID       openapi_types.UUID `json:"parentID"`
	ParentType     TaskType           `json:"parentType"`
	StartTimestamp time.Time          `json:"startTimestamp"`
	Vcpus          int                `json:"vcpus"`
}

// TaskStatus defines model for taskStatus.
type TaskStatus string

// TaskType defines model for taskType.
type TaskType string

// UpdateTaskInput defines model for updateTaskInput.
type UpdateTaskInput struct {
	Output *string     `json:"output"`
	Status *TaskStatus `json:"status,omitempty"`
}

// AgentHeartbeatJSONRequestBody defines body for AgentHeartbeat for application/json ContentType.
type AgentHeartbeatJSONRequestBody = AgentHeartbeatInput

// CreateTaskRecordJSONRequestBody defines body for CreateTaskRecord for application/json ContentType.
type CreateTaskRecordJSONRequestBody = TaskRecordInput

// UpdateTaskJSONRequestBody defines body for UpdateTask for application/json ContentType.
type UpdateTaskJSONRequestBody = UpdateTaskInput

// RequestEditorFn  is the function signature for the RequestEditor callback function
type RequestEditorFn func(ctx context.Context, req *http.Request) error

// Doer performs HTTP requests.
//
// The standard http.Client implements this interface.
type HttpRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client which conforms to the OpenAPI3 specification for this service.
type Client struct {
	// The endpoint of the server conforming to this interface, with scheme,
	// https://api.deepmap.com for example. This can contain a path relative
	// to the server, such as https://api.deepmap.com/dev-test, and all the
	// paths in the swagger spec will be appended to the server.
	Server string

	// Doer for performing requests, typically a *http.Client with any
	// customized settings, such as certificate chains.
	Client HttpRequestDoer

	// A list of callbacks for modifying requests which are generated before sending over
	// the network.
	RequestEditors []RequestEditorFn
}

// ClientOption allows setting custom parameters during construction
type ClientOption func(*Client) error

// Creates a new Client, with reasonable defaults
func NewClient(server string, opts ...ClientOption) (*Client, error) {
	// create a client with sane default values
	client := Client{
		Server: server,
	}
	// mutate client and add all optional params
	for _, o := range opts {
		if err := o(&client); err != nil {
			return nil, err
		}
	}
	// ensure the server URL always has a trailing slash
	if !strings.HasSuffix(client.Server, "/") {
		client.Server += "/"
	}
	// create httpClient, if not already present
	if client.Client == nil {
		client.Client = &http.Client{}
	}
	return &client, nil
}

// WithHTTPClient allows overriding the default Doer, which is
// automatically created using http.Client. This is useful for tests.
func WithHTTPClient(doer HttpRequestDoer) ClientOption {
	return func(c *Client) error {
		c.Client = doer
		return nil
	}
}

// WithRequestEditorFn allows setting up a callback function, which will be
// called right before sending the request. This can be used to mutate the request.
func WithRequestEditorFn(fn RequestEditorFn) ClientOption {
	return func(c *Client) error {
		c.RequestEditors = append(c.RequestEditors, fn)
		return nil
	}
}

// The interface specification for the client above.
type ClientInterface interface {
	// AgentHeartbeatWithBody request with any body
	AgentHeartbeatWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	AgentHeartbeat(ctx context.Context, body AgentHeartbeatJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	// Health request
	Health(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error)

	// CreateTaskRecordWithBody request with any body
	CreateTaskRecordWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	CreateTaskRecord(ctx context.Context, body CreateTaskRecordJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	// UpdateTaskWithBody request with any body
	UpdateTaskWithBody(ctx context.Context, taskName TaskName, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	UpdateTask(ctx context.Context, taskName TaskName, body UpdateTaskJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	// WorkerAPIPing request
	WorkerAPIPing(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error)
}

func (c *Client) AgentHeartbeatWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAgentHeartbeatRequestWithBody(c.Server, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) AgentHeartbeat(ctx context.Context, body AgentHeartbeatJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAgentHeartbeatRequest(c.Server, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) Health(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewHealthRequest(c.Server)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) CreateTaskRecordWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewCreateTaskRecordRequestWithBody(c.Server, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) CreateTaskRecord(ctx context.Context, body CreateTaskRecordJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewCreateTaskRecordRequest(c.Server, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) UpdateTaskWithBody(ctx context.Context, taskName TaskName, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewUpdateTaskRequestWithBody(c.Server, taskName, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) UpdateTask(ctx context.Context, taskName TaskName, body UpdateTaskJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewUpdateTaskRequest(c.Server, taskName, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) WorkerAPIPing(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewWorkerAPIPingRequest(c.Server)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

// NewAgentHeartbeatRequest calls the generic AgentHeartbeat builder with application/json body
func NewAgentHeartbeatRequest(server string, body AgentHeartbeatJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewAgentHeartbeatRequestWithBody(server, "application/json", bodyReader)
}

// NewAgentHeartbeatRequestWithBody generates requests for AgentHeartbeat with any type of body
func NewAgentHeartbeatRequestWithBody(server string, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/agent/heartbeat")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	return req, nil
}

// NewHealthRequest generates requests for Health
func NewHealthRequest(server string) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/health")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// NewCreateTaskRecordRequest calls the generic CreateTaskRecord builder with application/json body
func NewCreateTaskRecordRequest(server string, body CreateTaskRecordJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewCreateTaskRecordRequestWithBody(server, "application/json", bodyReader)
}

// NewCreateTaskRecordRequestWithBody generates requests for CreateTaskRecord with any type of body
func NewCreateTaskRecordRequestWithBody(server string, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/task/record")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	return req, nil
}

// NewUpdateTaskRequest calls the generic UpdateTask builder with application/json body
func NewUpdateTaskRequest(server string, taskName TaskName, body UpdateTaskJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewUpdateTaskRequestWithBody(server, taskName, "application/json", bodyReader)
}

// NewUpdateTaskRequestWithBody generates requests for UpdateTask with any type of body
func NewUpdateTaskRequestWithBody(server string, taskName TaskName, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "taskName", runtime.ParamLocationPath, taskName)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/task/%s/update", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	return req, nil
}

// NewWorkerAPIPingRequest generates requests for WorkerAPIPing
func NewWorkerAPIPingRequest(server string) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/workerapiping")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

func (c *Client) applyEditors(ctx context.Context, req *http.Request, additionalEditors []RequestEditorFn) error {
	for _, r := range c.RequestEditors {
		if err := r(ctx, req); err != nil {
			return err
		}
	}
	for _, r := range additionalEditors {
		if err := r(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// ClientWithResponses builds on ClientInterface to offer response payloads
type ClientWithResponses struct {
	ClientInterface
}

// NewClientWithResponses creates a new ClientWithResponses, which wraps
// Client with return type handling
func NewClientWithResponses(server string, opts ...ClientOption) (*ClientWithResponses, error) {
	client, err := NewClient(server, opts...)
	if err != nil {
		return nil, err
	}
	return &ClientWithResponses{client}, nil
}

// WithBaseURL overrides the baseURL.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) error {
		newBaseURL, err := url.Parse(baseURL)
		if err != nil {
			return err
		}
		c.Server = newBaseURL.String()
		return nil
	}
}

// ClientWithResponsesInterface is the interface specification for the client with responses above.
type ClientWithResponsesInterface interface {
	// AgentHeartbeatWithBodyWithResponse request with any body
	AgentHeartbeatWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AgentHeartbeatResponse, error)

	AgentHeartbeatWithResponse(ctx context.Context, body AgentHeartbeatJSONRequestBody, reqEditors ...RequestEditorFn) (*AgentHeartbeatResponse, error)

	// HealthWithResponse request
	HealthWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*HealthResponse, error)

	// CreateTaskRecordWithBodyWithResponse request with any body
	CreateTaskRecordWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*CreateTaskRecordResponse, error)

	CreateTaskRecordWithResponse(ctx context.Context, body CreateTaskRecordJSONRequestBody, reqEditors ...RequestEditorFn) (*CreateTaskRecordResponse, error)

	// UpdateTaskWithBodyWithResponse request with any body
	UpdateTaskWithBodyWithResponse(ctx context.Context, taskName TaskName, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*UpdateTaskResponse, error)

	UpdateTaskWithResponse(ctx context.Context, taskName TaskName, body UpdateTaskJSONRequestBody, reqEditors ...RequestEditorFn) (*UpdateTaskResponse, error)

	// WorkerAPIPingWithResponse request
	WorkerAPIPingWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*WorkerAPIPingResponse, error)
}

type AgentHeartbeatResponse struct {
	Body         []byte
	HTTPResponse *http.Response
}

// Status returns HTTPResponse.Status
func (r AgentHeartbeatResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

// StatusCode returns HTTPResponse.StatusCode
func (r AgentHeartbeatResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type HealthResponse struct {
	Body         []byte
	HTTPResponse *http.Response
}

// Status returns HTTPResponse.Status
func (r HealthResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

// StatusCode returns HTTPResponse.StatusCode
func (r HealthResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type CreateTaskRecordResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON201      *TaskRecordCreationOutput
}

// Status returns HTTPResponse.Status
func (r CreateTaskRecordResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

// StatusCode returns HTTPResponse.StatusCode
func (r CreateTaskRecordResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type UpdateTaskResponse struct {
	Body         []byte
	HTTPResponse *http.Response
}

// Status returns HTTPResponse.Status
func (r UpdateTaskResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

// StatusCode returns HTTPResponse.StatusCode
func (r UpdateTaskResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type WorkerAPIPingResponse struct {
	Body         []byte
	HTTPResponse *http.Response
}

// Status returns HTTPResponse.Status
func (r WorkerAPIPingResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

// StatusCode returns HTTPResponse.StatusCode
func (r WorkerAPIPingResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

// AgentHeartbeatWithBodyWithResponse request with arbitrary body returning *AgentHeartbeatResponse
func (c *ClientWithResponses) AgentHeartbeatWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AgentHeartbeatResponse, error) {
	rsp, err := c.AgentHeartbeatWithBody(ctx, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAgentHeartbeatResponse(rsp)
}

func (c *ClientWithResponses) AgentHeartbeatWithResponse(ctx context.Context, body AgentHeartbeatJSONRequestBody, reqEditors ...RequestEditorFn) (*AgentHeartbeatResponse, error) {
	rsp, err := c.AgentHeartbeat(ctx, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAgentHeartbeatResponse(rsp)
}

// HealthWithResponse request returning *HealthResponse
func (c *ClientWithResponses) HealthWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*HealthResponse, error) {
	rsp, err := c.Health(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseHealthResponse(rsp)
}

// CreateTaskRecordWithBodyWithResponse request with arbitrary body returning *CreateTaskRecordResponse
func (c *ClientWithResponses) CreateTaskRecordWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*CreateTaskRecordResponse, error) {
	rsp, err := c.CreateTaskRecordWithBody(ctx, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseCreateTaskRecordResponse(rsp)
}

func (c *ClientWithResponses) CreateTaskRecordWithResponse(ctx context.Context, body CreateTaskRecordJSONRequestBody, reqEditors ...RequestEditorFn) (*CreateTaskRecordResponse, error) {
	rsp, err := c.CreateTaskRecord(ctx, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseCreateTaskRecordResponse(rsp)
}

// UpdateTaskWithBodyWithResponse request with arbitrary body returning *UpdateTaskResponse
func (c *ClientWithResponses) UpdateTaskWithBodyWithResponse(ctx context.Context, taskName TaskName, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*UpdateTaskResponse, error) {
	rsp, err := c.UpdateTaskWithBody(ctx, taskName, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseUpdateTaskResponse(rsp)
}

func (c *ClientWithResponses) UpdateTaskWithResponse(ctx context.Context, taskName TaskName, body UpdateTaskJSONRequestBody, reqEditors ...RequestEditorFn) (*UpdateTaskResponse, error) {
	rsp, err := c.UpdateTask(ctx, taskName, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseUpdateTaskResponse(rsp)
}

// WorkerAPIPingWithResponse request returning *WorkerAPIPingResponse
func (c *ClientWithResponses) WorkerAPIPingWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*WorkerAPIPingResponse, error) {
	rsp, err := c.WorkerAPIPing(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseWorkerAPIPingResponse(rsp)
}

// ParseAgentHeartbeatResponse parses an HTTP response from a AgentHeartbeatWithResponse call
func ParseAgentHeartbeatResponse(rsp *http.Response) (*AgentHeartbeatResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &AgentHeartbeatResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	return response, nil
}

// ParseHealthResponse parses an HTTP response from a HealthWithResponse call
func ParseHealthResponse(rsp *http.Response) (*HealthResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &HealthResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	return response, nil
}

// ParseCreateTaskRecordResponse parses an HTTP response from a CreateTaskRecordWithResponse call
func ParseCreateTaskRecordResponse(rsp *http.Response) (*CreateTaskRecordResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &CreateTaskRecordResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 201:
		var dest TaskRecordCreationOutput
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON201 = &dest

	}

	return response, nil
}

// ParseUpdateTaskResponse parses an HTTP response from a UpdateTaskWithResponse call
func ParseUpdateTaskResponse(rsp *http.Response) (*UpdateTaskResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &UpdateTaskResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	return response, nil
}

// ParseWorkerAPIPingResponse parses an HTTP response from a WorkerAPIPingWithResponse call
func ParseWorkerAPIPingResponse(rsp *http.Response) (*WorkerAPIPingResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &WorkerAPIPingResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	return response, nil
}