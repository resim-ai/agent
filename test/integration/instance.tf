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

  iam_instance_profile = aws_iam_instance_profile.profile.name

  tags = {
    Name = "agent-test"
  }

  user_data = <<-EOF
              #!/bin/bash
              # Setup Cloudwatch
              yum update -y
              yum install -y awslogs amazon-cloudwatch-agent
              cat <<EOC > /etc/awslogs/awslogs.conf
                  [general]
                  state_file = /var/lib/awslogs/agent-state
                  use_gzip_http_content_encoding = true
                  [/var/log/messages]
                  log_group_name = agent_testing
                  log_stream_name = /ec2/{instance_id}/var/log/messages
                  file = /var/log/messages
                  datetime_format = %Y-%m-%d %H:%M:%S
                  buffer_duration = 5000
                  initial_position = start_of_file
                  [/opt/application/server.log]
                  log_group_name = agent_testing
                  log_stream_name = /ec2/{instance_id}/opt/application/server.log
                  file = /opt/application/server.log
                  datetime_format = %Y-%m-%d %H:%M:%S
                  initial_position = start_of_file
                  time_zone = UTC
                  encoding = utf_8
                  buffer_duration = 5000
              EOC

              systemctl start awslogsd
              systemctl enable awslogsd.service

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

resource "aws_iam_policy" "this" {
  name        = "ec2_cloudwatch_policy"
  description = "EC2 CloudWatch Agent Policy"

  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "logs:CreateLogGroup",
                "logs:CreateLogStream",
                "logs:PutLogEvents",
                "logs:DescribeLogStreams"
            ],
            "Resource": [
                "arn:aws:logs:*:*:*"
            ]
        }
    ]
}
EOF
}

resource "aws_iam_role" "this" {
  name               = "EC2CloudWatchAccessRole"
  assume_role_policy = <<EOF
{
 "Version": "2012-10-17",
 "Statement": [
   {
     "Action": "sts:AssumeRole",
     "Principal": {
       "Service": "ec2.amazonaws.com"
     },
     "Effect": "Allow",
     "Sid": ""
   }
 ]
}
EOF
}

resource "aws_iam_policy_attachment" "this" {
  name       = "EC2CloudWatchAccessRoleAttachment"
  roles      = [aws_iam_role.this.name]
  policy_arn = aws_iam_policy.this.arn
}

resource "aws_iam_policy_attachment" "ssm" {
  name       = "EC2CloudWatchAccessSSM"
  roles      = [aws_iam_role.this.name]
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "profile" {
  name = "EC2CloudwatchInstanceProfile"
  role = aws_iam_role.this.name
}
