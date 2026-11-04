resource "devopser_variable" "region" {
  workspace_id = devopser_workspace.network.id
  key          = "region"
  value        = "us-east-1"
  category     = "terraform"
}

resource "devopser_variable" "api_key" {
  workspace_id = devopser_workspace.network.id
  key          = "API_KEY"
  value        = var.api_key
  category     = "env"
  sensitive    = true
}
