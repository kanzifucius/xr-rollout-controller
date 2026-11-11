## Summary

What changed, and why.

## Testing

- [ ] `make test`
- [ ] `make lint`

Note anything you could not run.

## Controller behaviour

Skip this for a docs-only change.

A behaviour change must keep these true: the controller only patches `spec.crossplane.compositionRevisionRef` on Manual XRs, it does not roll back, and it acknowledges triggers instead of deleting them. See `CONTRIBUTING.md` and `AGENTS.md`.
