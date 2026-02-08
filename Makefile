.PHONY: run graph-run graph-resume sessions test

run:
	@if [ -z "$(PROMPT)" ]; then \
		echo 'Usage: make run PROMPT="your prompt"'; \
		exit 1; \
	fi
	go run . run -- "$(PROMPT)"

graph-run:
	@if [ -z "$(PROMPT)" ]; then \
		echo 'Usage: make graph-run PROMPT="your prompt" [WORKFLOW=basic]'; \
		exit 1; \
	fi
	go run . graph-run $(if $(WORKFLOW),--workflow=$(WORKFLOW),) -- "$(PROMPT)"

graph-resume:
	@if [ -z "$(RUN_ID)" ]; then \
		echo 'Usage: make graph-resume RUN_ID="<run-id>" [WORKFLOW=basic]'; \
		exit 1; \
	fi
	go run . graph-resume $(if $(WORKFLOW),--workflow=$(WORKFLOW),) "$(RUN_ID)"

sessions:
	go run . sessions

test:
	go test ./...
