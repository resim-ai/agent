package testing

// Basic imports
import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/resim-ai/api-client/api"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/suite"
)

func (s *AgentTestSuite) SetupTest() {
	s.createTestProject()
	s.createTestSystem()
	s.createTestBranch()
	s.buildIDS3 = s.createBuild(experienceBuildURI)
	s.buildIDLocal = s.createBuild(viper.GetViper().GetString(LocalImageKey))
	s.createMetricsBuild()
}

func TestAgentTestSuite(t *testing.T) {
	viper.SetEnvPrefix(EnvPrefix)
	viper.AutomaticEnv()
	// This confusingly-named function defines the mapping from CLI parameter key to environment variable.
	// CLI parameters use kebab-case, and env vars use CAPITAL_SNAKE_CASE.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	authHost := viper.GetString(AuthHostKey)
	apiHost := viper.GetString(APIHostKey)
	tokenURL := fmt.Sprintf("%v/oauth/token", viper.GetString(AuthHostKey))
	username := viper.GetString(UsernameKey)
	password := viper.GetString(PasswordKey)
	experienceBucket := viper.GetString(ExperienceBucketKey)
	poolLabel := viper.GetString(PoolLabelsKey)

	s, err := NewAgentTestSuiteWithAPIClient(username, password, tokenURL, apiHost, authHost, experienceBucket, poolLabel)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiCheckTimeout)
	defer cancel()

	err = CheckAPIAvailability(ctx, apiHost, apiCheckInterval)
	if err != nil {
		t.Fatal(err)
	} else {
		fmt.Println("API is up")
	}

	suite.Run(t, s)
}

// Test the agent with a batch where the experiences are in S3
func (s *AgentTestSuite) TestAgentWithS3Experience() {
	realMetrics := true
	s.createS3TestExperience()
	batch := s.createAndAwaitBatch(s.buildIDS3, s.s3Experiences, s.sharedMemoryMb, false, realMetrics)
	jobsResponse, err := s.APIClient.ListJobsWithResponse(
		context.Background(),
		s.projectID,
		*batch.BatchID,
		&api.ListJobsParams{
			PageSize: Ptr(100),
		})

	s.NoError(err)
	s.Len(*jobsResponse.JSON200.Jobs, 1)
	expectedOutputFiles := ListExpectedOutputFiles(realMetrics)

	printLogs := *batch.Status != api.BatchStatusSUCCEEDED // we will print urls to logs if the batch did not succeed
	// Finally: for every job in the batch, list the logs:
	for _, job := range *jobsResponse.JSON200.Jobs {
		fmt.Printf("Checking logs for Job ID: %v\n", *job.JobID)
		listLogsResponse, err := s.APIClient.ListJobLogsForJobWithResponse(
			context.Background(),
			s.projectID,
			*batch.BatchID,
			*job.JobID,
			&api.ListJobLogsForJobParams{
				PageSize: Ptr(100),
			},
		)
		s.NoError(err)
		s.Len(*listLogsResponse.JSON200.Logs, len(expectedOutputFiles))
		for _, log := range *listLogsResponse.JSON200.Logs {
			s.Contains(expectedOutputFiles, *log.FileName)
			if printLogs {
				fmt.Printf(">> Log: %v , Presigned URL: %v \n", *log.FileName, *log.LogOutputLocation)
			}
		}
	}
}

// Test the agent with a batch where the experiences are baked into the image
func (s *AgentTestSuite) TestAgentWithLocalExperience() {
	realMetrics := false
	s.createLocalTestExperiences(nil)
	batch := s.createAndAwaitBatch(s.buildIDLocal, s.localExperiences, s.sharedMemoryMb, false, realMetrics)
	jobsResponse, err := s.APIClient.ListJobsWithResponse(
		context.Background(),
		s.projectID,
		*batch.BatchID,
		&api.ListJobsParams{
			PageSize: Ptr(100),
		})

	s.NoError(err)
	s.Len(*jobsResponse.JSON200.Jobs, 1)
	expectedOutputFiles := ListExpectedOutputFiles(realMetrics)

	printLogs := *batch.Status != api.BatchStatusSUCCEEDED // we will print urls to logs if the batch did not succeed
	// Finally: for every job in the batch, list the logs:
	for _, job := range *jobsResponse.JSON200.Jobs {
		fmt.Printf("Checking logs for Job ID: %v\n", *job.JobID)
		listLogsResponse, err := s.APIClient.ListJobLogsForJobWithResponse(
			context.Background(),
			s.projectID,
			*batch.BatchID,
			*job.JobID,
			&api.ListJobLogsForJobParams{
				PageSize: Ptr(100),
			},
		)
		s.NoError(err)
		s.Len(*listLogsResponse.JSON200.Logs, len(expectedOutputFiles))
		for _, log := range *listLogsResponse.JSON200.Logs {
			s.Contains(expectedOutputFiles, *log.FileName)
			if printLogs {
				fmt.Printf(">> Log: %v , Presigned URL: %v \n", *log.FileName, *log.LogOutputLocation)
			}
		}
	}
}

// Test the agent with a batch where the experiences are in S3
func (s *AgentTestSuite) TestDockerAgentWithS3Experience() {
	realMetrics := true
	s.createS3TestExperience()
	batch := s.createAndAwaitBatch(s.buildIDS3, s.s3Experiences, s.sharedMemoryMb, true, realMetrics)
	jobsResponse, err := s.APIClient.ListJobsWithResponse(
		context.Background(),
		s.projectID,
		*batch.BatchID,
		&api.ListJobsParams{
			PageSize: Ptr(100),
		})

	s.NoError(err)
	s.Len(*jobsResponse.JSON200.Jobs, 1)
	expectedOutputFiles := ListExpectedOutputFiles(realMetrics)

	printLogs := *batch.Status != api.BatchStatusSUCCEEDED // we will print urls to logs if the batch did not succeed
	// Finally: for every job in the batch, list the logs:
	for _, job := range *jobsResponse.JSON200.Jobs {
		fmt.Printf("Checking logs for Job ID: %v\n", *job.JobID)
		listLogsResponse, err := s.APIClient.ListJobLogsForJobWithResponse(
			context.Background(),
			s.projectID,
			*batch.BatchID,
			*job.JobID,
			&api.ListJobLogsForJobParams{
				PageSize: Ptr(100),
			},
		)
		s.NoError(err)
		s.Len(*listLogsResponse.JSON200.Logs, len(expectedOutputFiles))
		for _, log := range *listLogsResponse.JSON200.Logs {
			s.Contains(expectedOutputFiles, *log.FileName)
			if printLogs {
				fmt.Printf(">> Log: %v , Presigned URL: %v \n", *log.FileName, *log.LogOutputLocation)
			}
		}
	}
}
