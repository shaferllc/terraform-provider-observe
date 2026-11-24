default: test

# Unit tests (the acceptance tests skip without TF_ACC).
test:
	go test ./...

# Acceptance tests: real Terraform against the in-memory fake API. Needs terraform on PATH.
testacc:
	TF_ACC=1 CHECKPOINT_DISABLE=1 go test ./... -count=1 -v

# Refresh the embedded API description, then regenerate the registry docs. Resource attributes follow it.
spec:
	curl -fsS https://observe.live/api/openapi.json -o internal/provider/openapi.json

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name observe

install:
	go install .

.PHONY: default test testacc spec docs install
