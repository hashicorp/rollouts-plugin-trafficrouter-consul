# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

schema = 1
artifacts {
  zip = [
    "rollouts-plugin-trafficrouter-consul_${version}_darwin_amd64.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_darwin_arm64.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_freebsd_386.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_freebsd_amd64.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_linux_386.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_linux_amd64.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_linux_arm.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_linux_arm64.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_windows_386.zip",
    "rollouts-plugin-trafficrouter-consul_${version}_windows_amd64.zip",
  ]
  container = [
    "rollouts-plugin-trafficrouter-consul_release-default_linux_386_${version}_${commit_sha}.docker.dev.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_386_${version}_${commit_sha}.docker.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_amd64_${version}_${commit_sha}.docker.dev.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_amd64_${version}_${commit_sha}.docker.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_arm64_${version}_${commit_sha}.docker.dev.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_arm64_${version}_${commit_sha}.docker.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_arm_${version}_${commit_sha}.docker.dev.tar",
    "rollouts-plugin-trafficrouter-consul_release-default_linux_arm_${version}_${commit_sha}.docker.tar",
  ]
}
