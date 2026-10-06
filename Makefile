.PHONY: test vet goldens goldens-verify cascade premailer-outputs premailer-verify bench fmt differential bench-report bench-compare

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

CHROME_PATH ?= $(or $(shell command -v google-chrome),/Applications/Google Chrome.app/Contents/MacOS/Google Chrome)

cascade: ## render every fixture in headless Chrome as written and inlined; computed styles must match
	cd $(ORACLE) && npm ci --silent
	JUICER_CASCADE=1 CHROME_PATH="$(CHROME_PATH)" go test -run TestCascade -timeout 20m .

PREMAILER := testdata/oracle/premailer

premailer-outputs: ## regenerate premailer's outputs from the pinned gem (needs Ruby), then review the diff
	cd $(PREMAILER) && bundle install --quiet && LANG=en_US.UTF-8 bundle exec ruby generate.rb

premailer-verify: ## fail if committed premailer outputs disagree with the pinned gem
	cd $(PREMAILER) && bundle install --quiet && LANG=en_US.UTF-8 bundle exec ruby generate.rb --check

bench:
	go test -run "^$$" -bench . -benchmem -count 10

bench-report: ## this package and juice on the same documents, as a markdown table
	cd $(ORACLE) && npm ci --silent && node benchreport.mjs

BASE ?= origin/main
BENCH_DIR ?= $(or $(TMPDIR),/tmp)/go-juicer-bench
BENCH_RE := ^(BenchmarkInline|BenchmarkShapes)$$
BENCHSTAT := golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da

bench-compare: ## benchmark BASE (a git ref) against the working tree, alternating runs on this machine
	git worktree remove --force $(BENCH_DIR)/base 2>/dev/null || true
	rm -rf $(BENCH_DIR) && mkdir -p $(BENCH_DIR)
	git worktree add -q --detach $(BENCH_DIR)/base $(BASE)
	for i in 1 2 3 4 5 6; do \
		(cd $(BENCH_DIR)/base && go test -run '^$$' -bench '$(BENCH_RE)' -benchmem -count 1 .) >> $(BENCH_DIR)/base.txt && \
		go test -run '^$$' -bench '$(BENCH_RE)' -benchmem -count 1 . >> $(BENCH_DIR)/head.txt || exit 1; \
	done
	git worktree remove --force $(BENCH_DIR)/base
	go run $(BENCHSTAT) base=$(BENCH_DIR)/base.txt head=$(BENCH_DIR)/head.txt

SEED ?= 1
COUNT ?= 10000
CORPUS ?= $(CURDIR)/testdata/differential.jsonl

differential: ## compare with juice on COUNT generated documents from SEED, minimizing mismatches
	cd $(ORACLE) && npm ci --silent && node differential.mjs --seed $(SEED) --count $(COUNT) > $(CORPUS)
	JUICER_DIFFERENTIAL=$(CORPUS) JUICER_MINIMIZE=1 go test -run TestDifferential -v -timeout 60m .
