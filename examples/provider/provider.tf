terraform {
  required_providers {
    devopser = {
      source = "devopser-io/devopser"
    }
  }
}

# The token is an organization API key from Settings, API keys.
# Leave it out to use DEVOPSER_TOKEN, or the app.devopser.io entry in
# ~/.terraform.d/credentials.tfrc.json.
provider "devopser" {
  hostname = "app.devopser.io"
}
