data "devopser_github_app_installation" "gh" {
  name = "my-github-org"
}

resource "devopser_workspace" "network" {
  name              = "network"
  auto_apply        = true
  queue_all_runs    = false
  working_directory = "network"
  terraform_version = "1.9.4"

  vcs_repo {
    identifier                 = "my-github-org/infrastructure"
    branch                     = "main"
    github_app_installation_id = data.devopser_github_app_installation.gh.id
  }
}
