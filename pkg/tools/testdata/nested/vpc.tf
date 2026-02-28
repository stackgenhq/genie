resource "aws_vpc" "nested" {
  cidr_block = "172.16.0.0/16"
  
  tags = {
    Name = "Nested VPC"
  }
}
