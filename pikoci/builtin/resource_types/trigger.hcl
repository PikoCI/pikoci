# This resource type is handled internally by the worker via the
# pikoci://trigger source protocol. No check/pull/push blocks are needed.
resource_type "trigger" {
  source = "pikoci://trigger"
}
