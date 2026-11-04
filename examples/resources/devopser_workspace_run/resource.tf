# Applies the workspace when created and destroys its resources when this
# resource is destroyed. With manual_confirm = false the provider confirms the
# run itself; a destroy needs an infra:admin key from an organization admin.
resource "devopser_workspace_run" "network" {
  workspace_id = devopser_workspace.network.id

  apply {
    manual_confirm = false
    wait_for_run   = true
  }

  destroy {
    manual_confirm = false
    wait_for_run   = true
  }

  depends_on = [devopser_variable.region, devopser_workspace_settings.network]
}
