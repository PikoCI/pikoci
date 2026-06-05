runner_type "docker" {
  run {
    path = "docker"
    args = [
      "run", "--rm",
      "-v", "$WORKDIR:/workdir",
      "-w", "/workdir",
      "$env",
      "$args",
      "$image",
      "/bin/sh", "-ec", "command -v git >/dev/null && git config --global --add safe.directory '*'; $cmd",
    ]
  }
}
