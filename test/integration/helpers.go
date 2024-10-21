package testing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/resim-ai/api-client/api"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/oauth2"
)

const (
	// A pair of constants used for creating actual experience data for test experiences
	ExpectedExperienceNameFile       string = "experience_name.txt"
	ExpectedExperienceNameBase64File string = "experience_name.base64"

	experienceBuildURI string = "909785973729.dkr.ecr.us-east-1.amazonaws.com/rerun-end-to-end-test-experience-build:latest"

	// Output File Names
	TestMCAPFile string = "test.mcap"
	TestMP4File  string = "test.mp4"

	apiCheckTimeout  = 10 * time.Minute
	apiCheckInterval = 10 * time.Second

	APIHostKey          = "api-host"
	AuthHostKey         = "auth-host"
	LocalImageKey       = "local-image"
	PoolLabelsKey       = "pool-labels"
	UsernameKey         = "username"
	PasswordKey         = "password"
	AgentNameKey        = "name"
	ExperienceBucketKey = "experience-bucket"
	EnvPrefix           = "AGENT_TEST"
	LogLevelKey         = "log-level"
	devClientID         = "LLNl3xsbNLSd16gQyYsiEn3tbLDZo1gj"
	audience            = "https://api.resim.ai"
)

type AgentTestSuite struct {
	suite.Suite
	experienceBucket string
	APIClient        *api.ClientWithResponses
	AuthHost         string
	APIHost          string
	projectID        uuid.UUID
	systemID         uuid.UUID
	branchID         uuid.UUID
	buildIDS3        uuid.UUID
	buildIDLocal     uuid.UUID
	metricsBuildID   uuid.UUID
	s3Experiences    []uuid.UUID
	localExperiences []uuid.UUID
	poolLabels       api.PoolLabels
}

type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int32  `json:"expires_in"`
}

// Ptr takes its argument and returns a pointer to it.
func Ptr[T any](t T) *T {
	return &t
}

func ListExpectedOutputFiles() []string {
	return []string{
		TestMCAPFile,
		TestMP4File,
		"metrics.binproto",
		ExpectedExperienceNameFile,
		ExpectedExperienceNameBase64File,
		"experience-worker.log",
		"experience-container.log",
		"metrics-worker.log",
		"metrics-container.log",
		"test_config.json",
	}
}

func CheckAPIAvailability(ctx context.Context, endpoint string, interval time.Duration) error {
	// Parse the URL and strip off the path
	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout reached: API is not available at %s", endpoint)
		case <-ticker.C:
			// Try to send a GET request to the API endpoint
			resp, err := http.Get(baseURL)
			if err != nil {
				fmt.Println("Error connecting to API:", err)
			} else {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					fmt.Println("API is available:", baseURL)
					return nil
				} else {
					fmt.Printf("API response status: %d\n", resp.StatusCode)
				}
			}
		}
	}
}

func NewAgentTestSuiteWithAPIClient(
	username, password, tokenURL, apiHost, authHost, experienceBucket, poolLabel string,
) (*AgentTestSuite, error) {
	ctx := context.Background()
	payloadVals := url.Values{
		"grant_type": []string{"http://auth0.com/oauth/grant-type/password-realm"},
		"realm":      []string{"cli-users"},
		"username":   []string{username},
		"password":   []string{password},
		"audience":   []string{audience},
		"client_id":  []string{devClientID},
	}
	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(payloadVals.Encode()))

	req.Header.Add("content-type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal("error in auth: ", err)
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var tj tokenJSON
	err = json.Unmarshal(body, &tj)
	if err != nil {
		log.Fatal(err)
	}

	token := &oauth2.Token{
		AccessToken:  tj.AccessToken,
		TokenType:    tj.TokenType,
		RefreshToken: tj.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tj.ExpiresIn) * time.Second),
	}
	oauthClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	apiClient, err := api.NewClientWithResponses(
		apiHost,
		api.WithHTTPClient(oauthClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %v", err)
	}

	return &AgentTestSuite{
		localExperiences: []uuid.UUID{},
		s3Experiences:    []uuid.UUID{},
		APIClient:        apiClient,
		APIHost:          apiHost,
		AuthHost:         authHost,
		experienceBucket: experienceBucket,
		poolLabels: api.PoolLabels{
			poolLabel,
		},
	}, nil
}

func (s *AgentTestSuite) createTestProject() {
	// Create a project:
	createProjectRequest := api.CreateProjectInput{
		Name:        fmt.Sprintf("Test Project %v", uuid.New()),
		Description: "description",
	}
	createProjectResponse, err := s.APIClient.CreateProjectWithResponse(
		context.Background(),
		createProjectRequest,
	)
	if err != nil {
		s.T().Log("Unable to create project", "error", err)
		os.Exit(1)
	}
	project := createProjectResponse.JSON201
	s.projectID = project.ProjectID
}

func (s *AgentTestSuite) createTestSystem() {
	systemName := fmt.Sprintf("Test System %v", uuid.New())
	createSystemRequest := api.CreateSystemInput{
		Name:                       systemName,
		Description:                "ok",
		BuildGpus:                  0,
		BuildMemoryMib:             256,
		BuildVcpus:                 1,
		BuildSharedMemoryMb:        64,
		MetricsBuildGpus:           0,
		MetricsBuildMemoryMib:      256,
		MetricsBuildVcpus:          1,
		MetricsBuildSharedMemoryMb: 64,
	}
	createSystemResponse, err := s.APIClient.CreateSystemWithResponse(
		context.Background(),
		s.projectID,
		createSystemRequest,
	)
	if err != nil {
		slog.Error("Unable to create system", "error", err)
		os.Exit(1)
	}
	s.systemID = createSystemResponse.JSON201.SystemID
}

func (s *AgentTestSuite) createTestBranch() {
	branchName := fmt.Sprintf("Test Branch %v", uuid.New())
	createBranchRequest := api.CreateBranchInput{
		Name:       branchName,
		BranchType: api.CHANGEREQUEST,
	}
	createBranchResponse, err := s.APIClient.CreateBranchForProjectWithResponse(
		context.Background(),
		s.projectID,
		createBranchRequest,
	)
	if err != nil {
		slog.Error("Unable to create branch", "error", err)
		os.Exit(1)
	}
	s.branchID = createBranchResponse.JSON201.BranchID
}

func (s *AgentTestSuite) createBuild(imageURI string) uuid.UUID {
	buildDescription := "description"
	buildVersion := uuid.New().String()

	createRequest := api.CreateBuildForBranchInput{
		SystemID:    s.systemID,
		Description: &buildDescription,
		ImageUri:    imageURI,
		Version:     buildVersion,
	}
	createBuildResponse, err := s.APIClient.CreateBuildForBranchWithResponse(
		context.Background(),
		s.projectID,
		s.branchID,
		createRequest,
	)
	if err != nil {
		slog.Error("Unable to create build", "error", err)
		os.Exit(1)
	}
	return createBuildResponse.JSON201.BuildID
}

func (s *AgentTestSuite) createMetricsBuild() {
	imageURI := "public.ecr.aws/docker/library/hello-world:latest"
	metricsBuildName := fmt.Sprintf("Test Metrics Build %v", uuid.New())
	createMetricsBuildRequest := api.CreateMetricsBuildInput{
		Name:     metricsBuildName,
		Version:  uuid.New().String(),
		ImageUri: imageURI,
	}
	createMetricsBuildResponse, err := s.APIClient.CreateMetricsBuildWithResponse(
		context.Background(),
		s.projectID,
		createMetricsBuildRequest,
	)
	if err != nil {
		slog.Error("Unable to create metrics build", "error", err)
		os.Exit(1)
	}
	s.metricsBuildID = createMetricsBuildResponse.JSON201.MetricsBuildID
}

func (s *AgentTestSuite) createS3TestExperience() {
	// Create an experience:
	experienceName := fmt.Sprintf("Test Experience %v", uuid.New())
	// Generate a location and upload some experience files:
	testLocation := s.generateAndUploadExperience(context.Background(), experienceName)

	createExperienceRequest := api.CreateExperienceInput{
		Name:        experienceName,
		Description: "description",
		Location:    testLocation,
	}
	createExperienceResponse, err := s.APIClient.CreateExperienceWithResponse(
		context.Background(),
		s.projectID,
		createExperienceRequest,
	)
	if err != nil {
		slog.Error("Unable to create experience", "error", err)
		os.Exit(1)
	}
	s.s3Experiences = append(s.s3Experiences, createExperienceResponse.JSON201.ExperienceID)
}

func (s *AgentTestSuite) createLocalTestExperiences() {
	// Create an experience:
	experienceName1 := "experience_1"
	// experienceName2 := fmt.Sprintf("Test Experience %v", uuid.New())

	testLocation1 := "/test_experience_data/experience_1/"
	// testLocation2 := "/test_experience_data/experience_1"

	createExperienceRequest := api.CreateExperienceInput{
		Name:        experienceName1,
		Description: "description",
		Location:    testLocation1,
	}
	createExperienceResponse, err := s.APIClient.CreateExperienceWithResponse(
		context.Background(),
		s.projectID,
		createExperienceRequest,
	)
	if err != nil {
		slog.Error("Unable to create experience", "error", err)
		os.Exit(1)
	}
	s.localExperiences = append(s.localExperiences, createExperienceResponse.JSON201.ExperienceID)
}

// Generates an experience and uploads it to an s3 path
func (s *AgentTestSuite) generateAndUploadExperience(ctx context.Context, experienceName string) string {
	// Use the experience name to create a single file:
	// name.experience
	// Then also create a base64 encoded version of the name as
	// name.base64
	// Upload these to s3 in the test bucket:
	// test-bucket/experiences/{uniqueID}
	// Return the s3 path to the experience:

	testLocation := fmt.Sprintf("s3://%s/experiences/%s/", s.experienceBucket, uuid.New())
	locationURL, err := url.Parse(testLocation)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load aws configuration", "error", err)
		os.Exit(1)
	}
	uploader := manager.NewUploader(s3.NewFromConfig(cfg))

	data := []byte(experienceName)
	base64Name := Base64EncodeString(experienceName)
	// Upload the data to s3:
	uploadFile(ctx, uploader, ExpectedExperienceNameFile, locationURL, data)
	uploadFile(ctx, uploader, ExpectedExperienceNameBase64File, locationURL, base64Name)
	return testLocation
}

func uploadFile(
	ctx context.Context,
	uploader *manager.Uploader,
	filename string,
	locationURL *url.URL,
	data []byte,
) {
	// Get the location
	bucket := locationURL.Host
	key := path.Join(
		strings.TrimPrefix(locationURL.Path, "/"),
		filename,
	) // path starts with a leading /, which is bad
	slog.Info(fmt.Sprintf("Uploading to s3://%v/%v\n", bucket, key))
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		slog.Error("unable to upload file", "filename", filename, "error", err)
		os.Exit(1)
	}
}

func Base64EncodeString(input string) []byte {
	previous := []byte(input)
	encodedLen := base64.StdEncoding.EncodedLen(len(previous))
	base64Input := make([]byte, encodedLen)
	base64.StdEncoding.Encode(base64Input, previous)
	return base64Input
}

func (s *AgentTestSuite) createAndAwaitBatch(buildID uuid.UUID, experiences []uuid.UUID, isDocker bool) api.Batch {
	var poolLabels []string
	if isDocker {
		poolLabels = []string{
			s.poolLabels[0] + "-docker",
		}
	} else {
		poolLabels = s.poolLabels
	}
	// Create a batch:
	createBatchRequest := api.BatchInput{
		ExperienceIDs: &experiences,
		BuildID:       &buildID,
		PoolLabels:    &poolLabels,
		Parameters: &api.BatchParameters{
			"buildID":         buildID.String(),
			"repeatedBuildID": buildID.String(),
		},
	}
	createBatchRequest.MetricsBuildID = &s.metricsBuildID
	createBatchResponse, err := s.APIClient.CreateBatchWithResponse(
		context.Background(),
		s.projectID,
		createBatchRequest,
	)
	if err != nil {
		slog.Error("Unable to create batch", "error", err)
		os.Exit(1)
	}

	batch := *createBatchResponse.JSON201

	s.Eventually(func() bool {
		getResponse, err := s.APIClient.GetBatchWithResponse(
			context.Background(),
			s.projectID,
			*batch.BatchID,
		)
		s.NoError(err)
		require.Equal(s.T(), http.StatusOK, getResponse.StatusCode(), string(getResponse.Body))
		status := *getResponse.JSON200.Status
		complete := isComplete(status)
		if !complete {
			s.T().Logf("Waiting for batch completion, current status: %v", status)
		} else {
			s.T().Logf("Batch completed, with status: %v", status)
		}
		return complete
	}, 10*time.Minute, 10*time.Second)

	// Validate that it has SUCCEEDED:
	getBatchResponse, err := s.APIClient.GetBatchWithResponse(
		context.Background(),
		s.projectID,
		*batch.BatchID,
	)
	s.NoError(err)
	require.Equal(s.T(), http.StatusOK, getBatchResponse.StatusCode(), string(getBatchResponse.Body))
	s.Equal(api.BatchStatusSUCCEEDED, *getBatchResponse.JSON200.Status)
	return *getBatchResponse.JSON200
}

func isComplete(status api.BatchStatus) bool {
	return (status == api.BatchStatusSUCCEEDED || status == api.BatchStatusCANCELLED || status == api.BatchStatusERROR)
}
