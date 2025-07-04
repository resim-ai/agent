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
      version = "~> 5.70.0" # TODO: Unpin minor version when it's not broken
    }
    cloudinit = {
      source  = "hashicorp/cloudinit"
      version = "~> 2.0"
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
  description = "This should be either 'pr-xx' if triggered from the agent repo, 'staging' if triggered by the rerun repo on merge to main, or 'dev-env-pr-xx' if it's a PR in rerun"
  type        = string
}

variable "agent_password" {
  type = string
}

locals {
  # If this is a rerun PR, use the PR environment; otherwise use staging
  api_host = startswith(var.environment, "dev") ? "${var.environment}.agentapi.dev.resim.io" : "agentapi.resim.io"
}

data "aws_ami" "this" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "cloudinit_config" "config" {
  base64_encode = true

  part {
    filename     = "cloud-config.yaml"
    content_type = "text/cloud-config"
    content = templatefile(
      "${path.module}/templates/cloud-config.yaml.tftpl",
      {
        auth_host      = "https://resim-dev.us.auth0.com"
        api_host       = "https://${local.api_host}/agent/v1"
        pool_labels    = "agent-test-${terraform.workspace}"
        agent_name     = "barry"
        agent_version  = startswith(var.environment, "pr") ? terraform.workspace : "main"
        agent_username = "e2e.resim.ai"
        agent_password = var.agent_password

        cloudwatch_agent_config = base64encode(templatefile("${path.module}/templates/amazon-cloudwatch-agent.json", { environment = var.environment }))
      }
    )
  }
}

resource "aws_instance" "test_agent" {
  ami             = data.aws_ami.this.id
  instance_type   = "t3a.micro"
  subnet_id       = "subnet-068480ff23a430b87"
  security_groups = ["sg-02994ab0d8a58f1dc"]

  iam_instance_profile = aws_iam_instance_profile.profile.name

  tags = {
    Name = "agent-test"
  }

  user_data = data.cloudinit_config.config.rendered

  root_block_device {
    volume_type = "gp3"
  }
}

resource "aws_iam_policy" "this" {
  name        = "agent-test-cloudwatch-${terraform.workspace}"
  description = "Agent Test"

  policy = data.aws_iam_policy_document.this.json
}

data "aws_iam_policy_document" "this" {
  statement {
    sid = "Cloudwatch"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:PutRetentionPolicy",
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
    sid       = "GetAuthorizationToken"
    effect    = "Allow"
    resources = ["*"]
    actions   = ["ecr:GetAuthorizationToken"]
  }
  statement {
    sid    = "ECR"
    effect = "Allow"

    actions = [
      "ecr:DescribeRepositories",
      "ecr:GetRepositoryPolicy",
      "ecr:GetDownloadUrlForLayer",
      "ecr:ListImages",
      "ecr:Get*",
      "ecr:List*",
      "ecr:Describe*",
      "ecr:BatchGetImage",
      "ecr:BatchCheckLayerAvailability"
    ]
    resources = [
      "arn:aws:ecr:us-east-1:909785973729:repository/agent",
      "arn:aws:ecr:us-east-1:909785973729:repository/agent-test",
      "arn:aws:ecr:us-east-1:909785973729:repository/rerun-end-to-end-test-experience-build",
      "arn:aws:ecr:us-east-1:909785973729:repository/rerun-end-to-end-test-metrics-build",
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

  statement {
    sid = "Experiences"
    resources = [
      aws_s3_bucket.experiences.arn,
      "${aws_s3_bucket.experiences.arn}/*",
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


resource "aws_s3_bucket_public_access_block" "experiences" {
  bucket = aws_s3_bucket.experiences.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket" "experiences" {
  bucket = "resim-agent-test-${terraform.workspace}"

  force_destroy = true
}

data "aws_iam_policy_document" "resim" {

  statement {
    sid       = "ReSim-ReadWrite"
    effect    = "Allow"
    resources = [aws_s3_bucket.experiences.arn]

    actions = [
      "s3:*"
    ]

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:PrincipalOrgID"
      values   = ["o-nz0682unft"]
    }
  }
}

resource "aws_s3_bucket_policy" "experiences" {
  bucket = aws_s3_bucket.experiences.id
  policy = data.aws_iam_policy_document.resim.json
}
