include ../../../../Makefile.variables
include ../../../../common.env

TASK_FILE ?= dummy-task.md
BRANCH := dev
AGENT_DIR ?= $(shell cd ../.. && pwd)/agent
ALLOWED_TOOLS ?= Grep,Read

# MiniMax routing. Token is fetched live from the personal teamvault
# (secret key MOPmQL) so it never lands on disk; requires `teamvault-password`
# from github.com/bborbe/teamvault-utils. Override any var to point elsewhere.
MODEL ?= MiniMax-M2.7-highspeed
PROVIDER_API_KEY ?= $(shell teamvault-password --teamvault-config $(HOME)/.teamvault.json --teamvault-key MOPmQL 2>/dev/null)

generate-dummy-task:
	@echo '---' > $(TASK_FILE)
	@echo 'status: in_progress' >> $(TASK_FILE)
	@echo 'phase: execution' >> $(TASK_FILE)
	@echo 'title: Dummy Pi Task' >> $(TASK_FILE)
	@echo 'assignee: pi-agent' >> $(TASK_FILE)
	@echo '---' >> $(TASK_FILE)
	@echo '' >> $(TASK_FILE)
	@echo 'Compute 2 + 2. Return ONLY a JSON object, no prose around it:' >> $(TASK_FILE)
	@echo '' >> $(TASK_FILE)
	@echo '- `status`: "done"' >> $(TASK_FILE)
	@echo '- `message`: "the answer is N"' >> $(TASK_FILE)
	@echo "generated $(TASK_FILE)"

run-dummy-task: $(TASK_FILE)
	@go run -mod=mod . \
		-task-file=$(TASK_FILE) \
		-branch=$(BRANCH) \
		-agent-dir=$(AGENT_DIR) \
		-model='$(MODEL)' \
		-allowed-tools=$(ALLOWED_TOOLS) \
		-provider-api-key='$(PROVIDER_API_KEY)' \
		-v=2
