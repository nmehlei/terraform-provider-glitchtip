terraform {
  required_providers {
    glitchtip = {
      source = "nmehlei/glitchtip"
    }
  }
}

provider "glitchtip" {
  endpoint = "https://app.glitchtip.com"
  # token is sensitive - prefer the GLITCHTIP_TOKEN environment variable.
}
