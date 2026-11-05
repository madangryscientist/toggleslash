## What changed

<!-- One paragraph. Link the ticket or issue. -->

## Checklist

- [ ] The gate passes locally: `bun run typecheck && bun run lint && bun run format:check && bun run parity:check && bun run test`.
- [ ] If this changes a product surface, `docs/feature-parity.json` is updated and `bun run parity` was run.
      A `feat` commit must change the matrix; CI checks the commits between the base and `HEAD`.
- [ ] Tests or e2e cover the change.
- [ ] No `Co-Authored-By` trailer and no AI attribution line.
