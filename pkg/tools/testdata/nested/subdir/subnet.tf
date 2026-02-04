resource "aws_subnet" "private" {
  vpc_id     = aws_vpc.nested.id
  cidr_block = "172.16.1.0/24"
  
  tags = {
    Name = "Private Subnet"
  }
}
