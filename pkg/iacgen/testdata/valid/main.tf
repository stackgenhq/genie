terraform {
  required_version = ">= 1.0"
  
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-west-2"
}

resource "aws_s3_bucket" "example" {
  bucket = "my-example-bucket-${var.aws_region}"
  
  tags = {
    Name        = "Example bucket"
    Environment = "Test"
  }
}

output "bucket_name" {
  value = aws_s3_bucket.example.id
}
