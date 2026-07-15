resource "aws_security_group" "web" {
  name        = "web"
  cidr_blocks = var.allowed_cidrs
}
