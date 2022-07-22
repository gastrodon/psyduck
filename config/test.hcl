source "scyther" "data" {
  queue       = "data-holder"
  per_second  = 20
  destination = destination.trash.trash.id
}
