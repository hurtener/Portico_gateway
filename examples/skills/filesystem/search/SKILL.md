# Filesystem Search

Read-only access to a mounted filesystem.

1. `fs.list` enumerates a directory; supports glob patterns.
2. `fs.read` returns file contents (size-limited by the gateway).
3. You may NOT write or delete — those tools are not exposed.
