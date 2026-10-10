.PHONY: smoke
# One verification entrypoint shared by the developer, agents, and CI.
# Checks and tests the Go backend, builds both apps, starts them on local
# test ports, and exercises the comparison page and API through Next.js.
smoke:
	@bash scripts/smoke.sh
