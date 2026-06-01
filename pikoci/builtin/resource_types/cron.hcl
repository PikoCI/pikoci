resource_type "cron" {
  check "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      echo "[{\"date\":\"$(date)\"}]"
      EOT
    ]
  }
  pull "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      echo "date: $version_date"
      EOT
    ]
  }
  push "exec" { }
}
