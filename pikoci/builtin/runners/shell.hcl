runner_type "shell" {
  run {
    path = "$shell"
    args = ["-ec", "$cmd"]
  }
}
