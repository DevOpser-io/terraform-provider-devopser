# DevOpser Infrastructure Control provider for OpenTofu

`source = "devopser-io/devopser"` on the [OpenTofu registry](https://search.opentofu.org). It manages DevOpser Infrastructure Control the way `hashicorp/tfe` manages Terraform Cloud, with the same resource shapes: `devopser_workspace`, `devopser_variable`, `devopser_workspace_settings`, `devopser_workspace_run`, and the `devopser_outputs`, `devopser_workspace`, `devopser_variables` and `devopser_github_app_installation` data sources.

This is not the [DevOpser template](https://github.com/DevOpser-io/devopser), which shares the org name.

Built for and tested with OpenTofu only.

## Authentication
`token` in the provider block, else `DEVOPSER_TOKEN`, else the credentials `tofu login app.devopser.io` stores. `hostname` defaults to `app.devopser.io`.

License: MPL-2.0.
