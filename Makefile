.PHONY: test vet goldens goldens-verify premailer-outputs premailer-verify bench fmt differential

ORACLE := testdata/oracle

test: ## run the parity suite
	go test -race ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

goldens: ## regenerate expectations from pinned juice, then review the diff
	cd $(ORACLE) && npm ci --silent && node generate.mjs

goldens-verify: ## fail if committed goldens disagree with pinned juice
	cd $(ORACLE) && npm ci --silent && node generate.mjs --check

bench-shapes: ## juice across all seven document shapes
	cd $(ORACLE) && npm ci --silent && node shapebench.mjs

bench-node: ## juice on the same document, for comparison
	cd $(ORACLE) && npm ci --silent && node bench.mjs

PREMAILER := testdata/oracle/premailer

premailer-outputs: ## regenerate premailer's outputs from the pinned gem (needs Ruby), then review the diff
	cd $(PREMAILER) && bundle install --quiet && LANG=en_US.UTF-8 bundle exec ruby generate.rb

premailer-verify: ## fail if committed premailer outputs disagree with the pinned gem
	cd $(PREMAILER) && bundle install --quiet && LANG=en_US.UTF-8 bundle exec ruby generate.rb --check

bench:
	go test -run "^$$" -bench . -benchmem -count 10

SEED ?= 1
COUNT ?= 10000
CORPUS ?= $(CURDIR)/testdata/differential.jsonl

differential: ## compare with juice on COUNT generated documents from SEED, minimizing mismatches
	cd $(ORACLE) && npm ci --silent && node differential.mjs --seed $(SEED) --count $(COUNT) > $(CORPUS)
	JUICER_DIFFERENTIAL=$(CORPUS) JUICER_MINIMIZE=1 go test -run TestDifferential -v -timeout 60m .
