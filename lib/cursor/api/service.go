package cursor_api_sdk

/*
Package cursor_api_sdk glues app logic to Cursor’s HTTP/Connect APIs.

Prefer a functional style: helpers return resource/response structs with
enough context for the caller. OAuth poll/refresh, models, and agent runs
belong here at a high level; thin protobuf RPC wrappers live in endpoints.go.
*/
