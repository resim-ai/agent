# Agent Integration Tests

This directory has basic integration tests for the agent that will run on merge to `main` or a pull request. It goes a little something like this:

1. Build the agent binary and push to a private S3 bucket
2. Build a test build image with Hello World and some experiences baked in for the actual test
3. Bring up a test environment - deploy an Ubuntu machine in the default VPC
4. Download the agent binary associated with the PR
5. Run an abridged version of the 'Worker' test from the E2E test suite as follows:
   a. Create a `project`, `system`, `branch` and two `build`s, one with Hello World, and one with the test image from (2.) in the supplied ReRun environment
   b. Create two `experience`s for the S3 build, and two for the baked-in build
   c. Submit a `batch` for exch test case and wait
6. Report back to GitHub
7. Tear down the environment

Logs are streamed from the test host to Cloudwatch and can be found in the `rerun_dev` account under the [Log Group `/agent-integration-tests/`](https://us-east-1.console.aws.amazon.com/cloudwatch/home?region=us-east-1#logsV2:log-groups/log-group/$252Fagent-integration-tests$252F).

For now, the ReRun environment is hard-coded because the appropriate changes aren't in staging yet. Once we're in staging, we'll use that environment to test against.

Next step once this is merged and the above is complete is to call this workflow when relevant changes are made in
ReRun as well. In that scenario, we'll pass across the appropriate test environment from the ReRun PR.

## Local Dev

You can run these tests locally with a local running copy of the agent. Follow the instructions in the [main
README](../../README.md) on how to configure the agent. You need to set a `pool-label` to something unique for your
agent to pick up the test tasks. Almost certainly you will be running this in a different environment than the
default, so you will need to set the `api-host` and `auth-host` to the appropriate values for the environment you are
targeting.

You need to create a container image for the local experience test. Simply build the `Dockerfile` in this directory and
tag it, passing it into the test with the environment variable below. The `agent` assumes your docker daemon either has
the image locally or is authenticated to the appropriate registry.

Note that the ReSim app actively validates the image tag, so you need to have a full registry path.

You will also need to obtain the [Agent password](https://github.com/resim-ai/tf-auth0#agent-users) & [CLI password](https://github.com/resim-ai/tf-auth0#cli-users) for the `e2e.resim.ai` account by following the linked instructions. Note, this is a private repository.

Set up the agent config file:
```yaml
name: <unique-name>
pool-labels:
  - <unique-label>
username: e2e.resim.ai
password: <agent password>
api-host: https://agentapi.resim.io/agent/v1
auth-host: https://resim-dev.us.auth0.com
environment-variables:
  - THIS_IS_A=test
  - AGENT=true
mounts:
  - /tmp/blah:/tmp/agent/resim
```

Set the following environment variables:

```shell
export AWS_PROFILE=rerun_dev \
AWS_REGION=us-east-1 \
AGENT_TEST_NAME=<unique-name> \
AGENT_TEST_POOL_LABELS=<unique-label> \
AGENT_TEST_API_HOST=https://api.resim.io/v1/ \
AGENT_TEST_USERNAME=cli+e2e.resim.ai@resim.ai \
AGENT_TEST_AUTH_HOST=https://resim-dev.us.auth0.com \
AGENT_TEST_EXPERIENCE_BUCKET=<valid-bucket-name> \
AGENT_TEST_PASSWORD=<cli password> \
AGENT_TEST_LOCAL_IMAGE=909785973729.dkr.ecr.us-east-1.amazonaws.com/agent-test:<valid-image-tag>
```

Then run the tests:

Note: unless you run the dockerized agent too, the `TestDockerAgentWithS3Experience` test will fail.

```shell
go test -v ./test/integration
```

Note: if you are running this within the devcontainer, you will need to make sure there is a `/tmp/resim` directory on the host machine that the agent is running on and that the `.docker` directory is mounted into the container.