module "network" {
  source        = "./modules/network"
  allowed_cidrs = var.office_cidrs
}
