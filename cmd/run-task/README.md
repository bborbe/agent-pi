# Local CLI for agent-pi

File-based entry point for `agent-pi`. Reads a markdown task file, runs the agent, writes the updated content back to the same file. Mirrors the Kafka entry point (`../../main.go`) but uses file I/O instead of Kafka/CQRS.

Pi reads its own config from `$HOME/.pi`. Authenticate the pi CLI once interactively (`pi auth login`) before running tasks here — the saved credentials are picked up automatically.

## Run dummy task

```bash
make generate-dummy-task
make run-dummy-task
```
