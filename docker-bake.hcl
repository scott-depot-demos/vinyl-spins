# Single-platform build definitions.
#
# Examples:
#   DOCKER_USER=mydockeruser TAG=latest depot bake --push
#   DOCKER_USER=mydockeruser TAG=$(git rev-parse HEAD) depot bake --push
#
# Note: set `platforms` if you want to pin the build architecture.

variable "DOCKER_USER" {
  default = ""
}

variable "TAG" {
  default = ""
}

group "default" {
  targets = ["api", "ui"]
}

target "api" {
  context    = "."
  dockerfile = "backend/Dockerfile"
  platforms  = ["linux/amd64"]
  tags = ["${DOCKER_USER}/vst-api:${TAG}"]
  args = {
    DEMO_BIG = "0"
  }
}

target "ui" {
  context    = "frontend"
  dockerfile = "Dockerfile.prod"
  platforms  = ["linux/amd64"]
  tags = ["${DOCKER_USER}/vst-ui:${TAG}"]
  args = {
    DEMO_BIG = "0"
  }
}

