# ReSim Agent

The ReSim Agent allows ReSim jobs to be run on customer-controlled hosts.

It is designed to support the integration of Hardware-in-the-Loop (HiL) testing into your ReSim workflows.

See [the Agent page on our docs site](https://docs.resim.ai/guides/agent) for more details.

## Prerequisites

Install Docker (e.g. https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository)

```
sudo chgrp docker /run/containerd/containerd.sock
```

## Configuration

By default, the Agent loads config from `~/resim/config.yaml`, caches credentials in that directory, and logs to the same directory (as well as `stdout`), however these settings can be overridden by setting the following environment variables:

- `RESIM_AGENT_CONFIG_DIR` - to point to a different configuration and cache directory. The Agent will load configuration from `config.yaml` in this directory and cache credentials here.
- `RESIM_AGENT_LOG_DIR` - to point to a different _directory_ in which to write log files

The configuration file has the following options:

```
### Required
# Set a name for the agent which will be shown in ReSim
name: my-robot-arm
# Set the labels for which you would like this agent to run jobs (see below)
pool-labels: 
  - small
# These credentials for authenticating with the ReSim API will be provided by ReSim
username: 
password: 
### Optional 
# Log level - debug, info, warn, error (default: info)
log-level: info
# Size in MB of log file (default: 500), note that 3 compressed backups are kept
log-max-filesize: 200
```

Note that the `pool-labels` are an OR/ANY selection, that is, an agent running with the labels `big` and `small` will run jobs tagged with either of those labels.

## CI

`GO_VERSION` is set as global variable in the repository settings
