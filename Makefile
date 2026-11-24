# gitleaks runs from its module at one pinned version, never from PATH.
# The hooks and CI call these targets, so this is the only pin.
GITLEAKS := github.com/zricethezav/gitleaks/v8@v8.30.1
GITLEAKS_FLAGS := --config .gitleaks.toml --redact --verbose --no-banner

.PHONY: hooks leaks leaks-staged leaks-msg
.DEFAULT_GOAL := leaks

hooks: ## Point git at .githooks, once per clone
	git config core.hooksPath .githooks

# The history scan reports clean when it reads nothing, so it refuses to
# run without a commit to read.
leaks: ## gitleaks over the working tree, the full history and every commit message, as CI runs it
	go run $(GITLEAKS) dir . $(GITLEAKS_FLAGS)
	@test "$$(git rev-list --count HEAD 2>/dev/null || echo 0)" -gt 0 || { echo "leaks: no commits to scan" >&2; exit 1; }
	go run $(GITLEAKS) git . $(GITLEAKS_FLAGS)
	git log --all --format=%B | go run $(GITLEAKS) stdin $(GITLEAKS_FLAGS)

leaks-staged: ## gitleaks over the staged changes; the pre-commit hook runs this
	go run $(GITLEAKS) git --pre-commit --staged $(GITLEAKS_FLAGS)

leaks-msg: ## gitleaks over one commit message; the commit-msg hook runs this
	go run $(GITLEAKS) stdin $(GITLEAKS_FLAGS) < "$(MSG)"
