data "devopser_outputs" "network" {
  workspace = "network"
}

output "vpc_id" {
  value = data.devopser_outputs.network.nonsensitive_values.vpc_id
}
