<!-- markdownlint-disable MD041 -->

# Changelog

Changes and release notes for the ReSim agent

## v0.6.0 - 2025-06-12

- Ensures that containers are removed if there is a failure running the ReSim Worker

## v0.5.0 - 2025-03-28

- Added support for experience-specific container timeouts and a few robustness fixes.

## v0.4.0 - 2025-03-27

- Added `mounts` and `environment-variables` options to support mounting arbitrary volumes and environment variables into the test container

## v0.3.0 - 2025-03-19

- Added `aws-config-destination-dir` and `aws-config-source-dir` options to support mounting AWS config and credentials from the user running the agent (or another location) into the test container

## v0.2.6 - 2025-01-21

- Fixes missing parameter in integration test

## v0.2.5 - 2024-11-21

- Added support for using the [ECR Docker credential helper](https://github.com/awslabs/amazon-ecr-credential-helper/), assuming the appropriate credential mode is set in `~/.docker/config.json` and AWS is configured in `~/.aws`

## v0.2.4 - 2024-11-21

- Added `docker-network-mode` to enable running test workloads with either bridge (default) or host networking mode

## v0.2.3 - 2024-11-18

- Added privileged mode to enable running test workloads with elevated privileges

## v0.2.2 - 2024-11-01

- The agent now checks for updates and logs if a new version is available

## v0.2.1 - 2024-10-29

- The agent now bind mounts local Docker config into the workers it launches to support Docker auth

## v0.2.0 - 2024-10-29

- The agent has been refactored into a single go package to support `go install`

## v0.1.1 - 2024-10-28

- The environment variable prefix used by the agent has been changed to `RESIM_AGENT`

## v0.1.0 - 2024-10-24

- Initial release of the agent
