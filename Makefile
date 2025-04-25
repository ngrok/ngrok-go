# Make targets can be found in tools/make/*.mk
# Run `make help` to see more information about available targets


# Wrap `make` to force on the --warn-undefined-variables flag
# See: https://www.gnu.org/software/make/manual/make.html#Reading-Makefiles

# Ensure all targets (except _run) depend on _run, which handles
# dispatching to the correct sub-targets based on $(MAKECMDGOALS).
# This avoids redundant sub-Make invocations when multiple targets
# are specified on the command line.




# Optional environment flags (all accept: 1, true, yes)
#   MAKE_DRY_RUN=true   -> Prints commands without running them
#   MAKE_DEBUG=true     -> Shows detailed info about targets, variable expansion, rule matching, etc

.PHONY: _run
$(if $(MAKECMDGOALS),$(MAKECMDGOALS): %: _run)
_run:
	@$(MAKE) -f tools/make/_common.mk $(MAKECMDGOALS) \
		--warn-undefined-variables \
		$(if $(filter true 1 yes,$(MAKE_DRY_RUN)),-n) \
		$(if $(filter true 1 yes,$(MAKE_DEBUG)),--debug)
