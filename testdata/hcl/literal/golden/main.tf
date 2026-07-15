# Web tier security group.
resource "aws_security_group" "web" {
  name        = "web"
  description = "hotfix: widened for the on-call incident" # do not edit by hand
  vpc_id      = "vpc-0abc123"
}
