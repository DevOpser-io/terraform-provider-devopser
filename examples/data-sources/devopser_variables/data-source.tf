data "devopser_variables" "network" {
  workspace_id = data.devopser_workspace.network.id
}

output "env_keys" {
  value = [for v in data.devopser_variables.network.env : v.name]
}
