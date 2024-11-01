<!-- markdownlint-disable MD041 -->
# Changelog

Changes and release notes for the ReSim agent

## v0.2.2 - 2024-11-1

- The agent now checks for updates and logs if a new version is available

## v0.2.1 - 2024-10-29

- The agent now bind mounts local Docker config into the workers it launches to support Docker auth

## v0.2.0 - 2024-10-29

- The agent has been refactored into a single go package to support `go install`

## v0.1.1 - 2024-10-28

- The environment variable prefix used by the agent has been changed to `RESIM_AGENT`

## v0.1.0 - 2024-10-24

- Initial release of the agent