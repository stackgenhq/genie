# This file has a missing closing brace
resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
  
  tags = {
    Name = "Main VPC"
  # Missing closing brace for tags
}
