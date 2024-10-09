terraform {
  required_version = ">= 1.8.1"

  backend "s3" {
    bucket  = "resim-terraform"
    key     = "infrastructure/agent_tests/terraform.tfstate"
    region  = "us-east-1"
    profile = "rerun_dev"
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

data "template_cloudinit_config" "config" {
  base64_encode = true

  part {
    filename     = "cloud-config.yaml"
    content_type = "text/cloud-config"
    content = templatefile(
      "${path.module}/templates/cloud-config.yaml.tftpl",
      {
        auth_host      = "https://resim-dev.us.auth0.com"
        api_host       = "https://dev-env-pr-1269.agentapi.dev.resim.io/agent/v1"
        pool_labels    = "ec2-small"
        agent_name     = "barry"
        agent_version  = terraform.workspace
        agent_username = "e2e.resim.ai"
        agent_password = var.agent_password

        cloudwatch_agent_config = filebase64("${path.module}/templates/amazon-cloudwatch-agent.json")
      }
    )
  }
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

  user_data = data.template_cloudinit_config.config.rendered
}

resource "aws_iam_policy" "this" {
  name        = "agent-test-cloudwatch-${terraform.workspace}"
  description = "EC2 CloudWatch Agent Policy for Agent Test"

  policy = data.aws_iam_policy_document.this.json
}

data "aws_iam_policy_document" "this" {
  statement {
    sid = "Cloudwatch"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:DescribeLogStreams"
    ]
    resources = ["arn:aws:logs:*:*:*"]
    effect    = "Allow"
  }

  statement {
    sid = "AllowAgentCIToHead"
    resources = [
      "arn:aws:s3:::resim-binaries",
      "arn:aws:s3:::resim-binaries/*",
    ]
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetObject",
    ]
  }

  statement {
    sid = "AgentCIRW"
    resources = [
      "arn:aws:s3:::resim-binaries/agent/*"
    ]
    effect  = "Allow"
    actions = ["s3:*"]
  }
}

resource "aws_iam_role" "this" {
  name               = "agent-test-${terraform.workspace}"
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

resource "aws_iam_role_policy_attachment" "this" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.this.arn
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "profile" {
  name = "agent-test-${terraform.workspace}"
  role = aws_iam_role.this.name
}
