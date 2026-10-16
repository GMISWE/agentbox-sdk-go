# Examples

Each example is a standalone program. They create billable resources and
delete them before they exit; each sandbox also gets a pinned lifetime, so it
is reclaimed even if a run is interrupted.

```bash
export GMI_AGENTBOX_API_KEY="your-api-key"
# Optional: discovered from the catalogue when unset.
export GMI_AGENTBOX_IDC="..."
export GMI_AGENTBOX_INSTANCE_TYPE="..."
# Optional: defaults to docker.io/library/debian:bookworm-slim (needs bash).
export GMI_AGENTBOX_IMAGE="..."

go run ./examples/quickstart
```

| Example | Shows |
| --- | --- |
| [quickstart](quickstart) | The whole lifecycle, every step written out: pick a data center and SKU, register an Agent, wait for its template, launch, run a command, upload and download a file, clean up |
| [long-running-exec](long-running-exec) | Commands that outlast a synchronous wait (about 25 s): `Wait=false`, polling `GetExecution` by status, cancelling |
| [large-output](large-output) | Output beyond the 1 MiB that `Execute` returns: redirect to a file and download it, check `stdout_truncated` |
| [snapshot-pool](snapshot-pool) | Prepare one sandbox, snapshot it, launch several from the snapshot in parallel (`POOL_SIZE`, default 5) |
| [labels-and-cleanup](labels-and-cleanup) | Label sandboxes with metadata, find them by label, extend a lifetime, delete exactly what a run created |
| [pause-resume](pause-resume) | Pause and resume: what survives (home directory) and what does not (`/tmp`, processes) |

All examples except `quickstart` share the setup in
[internal/exampleutil](internal/exampleutil).
