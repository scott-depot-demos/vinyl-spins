# Multi-platform build definitions (amd64 + arm64).
#
# Examples:
#   DOCKER_USER=mydockeruser TAG=latest depot bake --push
#   DOCKER_USER=mydockeruser TAG=$(git rev-parse HEAD) PUSH_LATEST=true depot bake --push
#
# Note: `--load` does not support multi-platform outputs; use `--push` for multi-arch.

variable "DOCKER_USER" {
  default = ""
}

variable "TAG" {
  default = "latest"
}

variable "PUSH_LATEST" {
  default = false
}

group "default" {
  targets = ["api", "ui"]
}

target "api" {
  context    = "."
  dockerfile = "backend/Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags = PUSH_LATEST ? [
    "${DOCKER_USER}/vst-api:${TAG}",
    "${DOCKER_USER}/vst-api:latest",
  ] : [
    "${DOCKER_USER}/vst-api:${TAG}",
  ]
  args = {
    DEMO_BIG = "0"
  }
}

target "ui" {
  context    = "frontend"
  dockerfile = "frontend/Dockerfile.prod"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags = PUSH_LATEST ? [
    "${DOCKER_USER}/vst-ui:${TAG}",
    "${DOCKER_USER}/vst-ui:latest",
  ] : [
    "${DOCKER_USER}/vst-ui:${TAG}",
  ]
  args = {
    DEMO_BIG = "0"
  }
}

