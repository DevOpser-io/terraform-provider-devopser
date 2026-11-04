resource "devopser_workspace_settings" "network" {
  workspace_id        = devopser_workspace.network.id
  global_remote_state = true
}
