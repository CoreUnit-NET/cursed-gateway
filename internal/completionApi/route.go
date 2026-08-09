package completion_api

/*
Package completion_api serves the OpenAI-compatible HTTP surface.

route.go registers paths; *Handler.go files implement each endpoint.
Upstream Cursor calls go through lib/cursor/api. Response headers/status
must stay withheld until upstream init succeeds so account fallback can
still run (see delayed-header behavior in the project plan).
*/
