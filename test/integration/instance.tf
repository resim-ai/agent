terraform {
  required_version = ">= 1.8.1"

  backend "s3" {
    bucket  = "resim-terraform"
    key     = "infrastructure/agent_tests/terraform.tfstate"
    region  = "us-east-1"
    profile = "infrastructure"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  allowed_account_ids = ["840213157818"]
  profile             = "rerun_dev"
  region              = "us-east-1"

  default_tags {
    tags = {
      Environment = var.environment
      Service     = "rerun"
      Deployment  = terraform.workspace
    }
  }
}

variable "environment" {
  type    = string
  default = "dev"
}

variable "agent_password" {
  type = string
}

data "aws_ssm_parameter" "ecs_optimized_ami" {
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/recommended"
}

resource "aws_instance" "test_agent" {
  ami             = jsondecode(data.aws_ssm_parameter.ecs_optimized_ami.value)["image_id"]
  instance_type   = "t2.micro"
  subnet_id       = "subnet-068480ff23a430b87"
  security_groups = ["sg-02994ab0d8a58f1dc"]

  tags = {
    Name = "agent-test"
  }

  user_data = <<-EOF
              #!/bin/bash
              # Download and install the agent
              wget https://resim-binaries.s3.amazonaws.com/agent/agent-linux-amd64-${terraform.workspace} -O /usr/local/bin/agent
              chmod +x /usr/local/bin/agent
              # Set env vars
              export RERUN_AGENT_AUTH_HOST=https://resim-dev.us.auth0.com
              export RERUN_AGENT_API_HOST=https://dev-env-pr-1269.agentapi.dev.resim.io/agent/v1
              export RERUN_AGENT_NAME=barry
              export RERUN_AGENT_POOL_LABELS=ec2-small
              export RERUN_AGENT_USERNAME=e2e.resim.ai
              export RERUN_AGENT_PASSWORD=${var.agent_password}
              # Start the agent
              nohup agent &
              EOF
}
