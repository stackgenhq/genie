# Invalid attribute syntax
resource "aws_instance" "bad" {
  ami = 
  instance_type = "t2.micro"
}
