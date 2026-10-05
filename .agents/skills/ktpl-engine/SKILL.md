---
name: ktpl-engine
description: >-
  Use this skill when modifying ktpl's rendering engine: the iteration loop, snapshot/deferral logic, the ref
  function, identity resolution, floating objects, typed single-action rendering, or cycle/max-iteration errors.
---

# ktpl engine

Full semantics with edge cases: [references/semantics.md](references/semantics.md). Read it before changing behaviour.

## Mental model

```
load -> overlay merge -> register pending fields (S0) -> lint(S0)
for k := 1..max:
    snap := deepcopy(state)
    for f in pending (document order):
        out, err := exec(f, ref reads snap)
        switch: ok -> next.set(f.path, out); trace[f] = k
                errDeferred -> keep pending, record blocker
                other -> fail fast (FieldError)
    state = next; lint(state) -> fail fast
    if pending empty -> done
    if nothing rendered -> cycle error (list pending + blockers)
    if k == stopAfter -> emit partial, exit 0
pending left -> max-iterations error
```

## Checklist for any engine change

1. Does it keep **determinism**? (no map iteration for ordering, sequential exec)
2. Does `ref` still read **only the snapshot**?
3. Does deferral trigger on target, any **ancestor on the path**, and any **descendant** that is pending?
4. Are floating objects still **unreferenceable**, including from themselves?
5. Is a rendered value inserted as a node and **never re-registered** as pending?
6. Does every new error carry identity, path, `file:line` (and iteration)?
7. Add or update an `examples/` case (skill `ktpl-golden-tests`) and a unit test in `internal/engine`.

## Verify

```bash
go test -race ./internal/engine/... ./internal/tmpl/...
go test ./internal/cli -run TestGolden
```
