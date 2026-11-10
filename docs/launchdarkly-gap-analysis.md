# ToggleSlash vs LaunchDarkly: what is left

A comparison of what this repository ships today against what LaunchDarkly offers, as documented in
November 2026. The point is not to copy LaunchDarkly. It is to separate the gaps that block real use
from the gaps that are simply a different product.

> The maintained, per-capability matrix is [`feature-parity.md`](feature-parity.md). This page is the
> narrative deep dive behind it.

**Method.** I read this repository's `README.md`, `AGENTS.md`, `docs/architecture.md`,
`docs/evaluation.md`, `docs/api.md`, `docs/sdk.md` and the committed OpenAPI document, then mapped
each LaunchDarkly product area from its public documentation and pricing page. LaunchDarkly numbers
below are the vendor's published claims, not measurements. ToggleSlash numbers come from the tree.

**Short version.** We have built the **evaluation kernel and the admin CRUD** well. LaunchDarkly's
product is the **platform around that kernel**: 25+ SDKs, release automation, experimentation,
observability, AI configuration, governance and a globally distributed delivery layer. On the core
flag primitive we are maybe two-thirds of the way to a credible small-team alternative. On the
product surface a buyer compares, we are much closer to the beginning.

---

## 1. The two products

| | LaunchDarkly (2026) | ToggleSlash (today) |
| --- | --- | --- |
| What it is | A hosted runtime-control platform: flags, release automation (Release Autopilot), experimentation, observability, AgentControl for LLM apps, and enterprise governance | A self-hosted, MIT-licensed feature-flag service: one C# server, a TypeScript engine, a TypeScript server SDK, a CLI, a dashboard and a site |
| Delivery | SaaS, global edge, 70T+ evaluations/day, <200 ms config propagation, 99.99% claimed | One process with SQLite behind it; Docker/compose for self-hosting; no published image |
| SDKs | 25+ native SDKs across server, client, mobile, edge and AI, plus OpenFeature providers | One server-side TypeScript SDK; the C# evaluator is used only inside the server |
| Integrations | 80+ (GitHub, GitLab, Slack, Teams, Datadog, Sentry, Segment, Terraform, warehouses, CDNs, MCP, and more) | OpenAPI document, a partial CLI, and the dashboard. No Terraform, MCP, GitHub app or chat integration |
| Commercial model | Free Developer tier, pay-as-you-go Foundation, custom Enterprise; no per-seat pricing | MIT, self-hosted, no vendor, no usage meter |
| Governance | SSO/SAML, SCIM, custom RBAC, teams, views, approvals, unlimited audit history, critical environments, SOC 2 / ISO 27001 / ISO 27701 / FedRAMP Moderate | One bootstrap admin token, hashed SDK keys, an audit log, no user accounts |

The honest framing: **LaunchDarkly is a control plane; ToggleSlash is a flag server.** Most of what
follows is the control plane.

---

## 2. Scorecard

Maturity: 0 none, 1 minimal, 2 usable for a small team, 3 strong, 4 LaunchDarkly-class.

| Area | ToggleSlash | LaunchDarkly | The gap in one sentence |
| --- | ---: | ---: | --- |
| Evaluation semantics | 2.5 | 4 | Rigorous and cross-language-tested, but one context kind, eight operators and no experiments |
| Targeting and segments | 2 | 4 | Rules, prerequisites and segments exist; list/big/synced segments, attribute rollouts, bulk targeting and 5,000-rule limits do not |
| Flag model and lifecycle | 2 | 4 | Multivariate flags and archive/restore exist; templates, clone, compare/copy and code references do not |
| Release workflow | 0.5 | 4 | No approvals, scheduling, progressive/guarded rollouts, triggers, workflows or release policies |
| SDKs and delivery | 1 | 4 | One server SDK against 25+ SDKs, no browser/mobile/edge SDK, no relay, no persistent store, no bootstrap/offline mode |
| Experimentation and metrics | 0 | 4 | Daily usage counts only; no event stream, no metrics, no assignment, no statistics, no warehouse |
| Governance and security | 1 | 4 | One admin token and an audit log; no users, roles, SSO, SCIM, teams or approval policy |
| Integrations and automation | 1 | 4 | OpenAPI and a partial CLI; no Terraform, MCP, GitHub app, chat, data export or warehouse |
| Observability | 0 | 4 | None; LaunchDarkly has session replay, errors, logs, traces, metrics and alerts |
| AI / AgentControl | 0 | 4 | None; LaunchDarkly configures prompts, models, evals and agent graphs |
| Operations and scale | 1.5 | 4 | SQLite, one process, in-memory rate limits, no HA, no multi-region; LaunchDarkly sells global delivery as the product |
| Openness / self-hosting | 4 | 1 | MIT and self-hosted, which LaunchDarkly is not (its Relay Proxy is open source; the platform is not) |

Rough overall: **~65% of the core flag primitive, ~15 to 20% of the product surface** a LaunchDarkly
buyer evaluates.

---

## 3. Core flag model and evaluation

This is our strongest area, so it is worth being precise about what "parity" would mean.

### 3.1 Contexts

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Context object | `{ kind, key, ...attributes }`; many kinds (user, org, device, custom) | `{ key, attributes }`; **one kind** |
| Multi-context | `kind: "multi"` combining several kinds in one evaluation | No |
| Context kinds | First-class resources with attributes, archive/restore | No |
| Attribute references | `/address/city` for nested objects | Yes, a leading / reads a path; a dot is part of the literal name |
| Built-in attributes | key, kind, name, email, firstName, lastName, country, ip, avatar, anonymous | key only |
| Private attributes | Per-context or global; not stored/sent in events | No (the server stores no contexts at all) |
| Anonymous contexts | Supported and excluded from the Contexts list | No concept |
| Contexts list / detail | Stored and browsable, used for individual targeting and segments | Deliberately none: no end-user context is stored |

The last row is a real design fork, not just a missing feature. LaunchDarkly stores contexts so it can
show them, target them individually, build list-based segments and attribute experiments. We store
counts only, which is a strong privacy property but rules out a whole class of features. Any move
toward list-based segments or experiments forces a decision about storing context keys, even if not
attributes.

**What is left:** multi-context and context kinds, built-in attributes,
private/anonymous semantics (or a documented equivalent), and a decision on context storage.

### 3.2 Targeting operators

| Operator family | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Exact match | `in` over string/number/boolean/date | `in` over string/number/boolean |
| String | contains, startsWith, endsWith | contains, startsWith, endsWith |
| Regex | `matches` (engine varies by SDK; Rust/Go on the backend for client-side) | **None, by design** (ReDoS and cross-language semantics) |
| Numeric order | greaterThan, lessThan, greaterThanOrEqual, lessThanOrEqual | `lt`, `lte`, `gt`, `gte` (also string order) |
| Date | before, after (RFC-3339 or Unix ms) | No dedicated date op; ISO-8601 strings order correctly with `lt`/`gt` |
| Semantic version | semVerEqual, semVerGreaterThan, semVerLessThan (+ negate for ≥/≤) | None |
| Segments | `segmentMatch` | `segmentMatch` |
| Negation | `negate` | `negate`, with a documented missing-attribute rule |
| Limits | 5,000 rules and 50,000 values per flag | No explicit rule/value limit; payload size is the practical bound |

Our evaluation spec is unusually rigorous: it pins byte-level string comparison, IEEE-754 behaviour,
lone surrogates, missing-attribute/negate semantics and bucketing, and runs the same vectors against
two independent implementations. That is better than most SDKs manage. But the operator set is
deliberately small, and regex/semver/date are the three gaps users hit first.

**What is left:** semantic version and date operators are cheap and portable. Regex is the hard one:
if we add it, use a linear-time engine (RE2-style) on both sides or keep it server-side only, and
document the divergence. Do not adopt each language's native regex and call it parity.

### 3.3 Segments

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Rule-based segments | Yes | Yes (attribute clauses, included/excluded, weighted rules) |
| List-based (≤15k) | Yes | Included/excluded lists, no bulk import |
| Large list-based (≤1M, CSV) | Yes | No |
| Synced segments | Amplitude, Fivetran/Census, Heap, Hightouch, RudderStack, Tealium, Segment, Zeotap | No |
| Segment nesting | Not inside a segment (same as ours) | No |
| Segment versioning | Change history and approvals | Compare-and-set on a version; no approvals |
| Segment size in payload | Big segments delivered separately / evaluated server-side | Whole segment in the ruleset |

Our segment model is sound but local: membership is a list you type, not a list you import or sync.

**What is left:** bulk CSV import, synced segments, and a strategy for large
segments that does not bloat every ruleset.

### 3.4 Rollouts and bucketing

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Percentage rollout | Yes, optionally by context kind and attribute | Yes, by context key only |
| Consistent bucketing | Yes (flag/segment salt, context key or attribute) | Yes, spec'd SHA-256/100000 bucket |
| Progressive rollout | Automatic increase over time | No |
| Guarded rollout | Increase while monitoring metrics, auto-rollback | No |
| Audience-based release | Sequenced audiences with checks | No |
| Experiments | Assignment + analysis | No |
| Rollout limits | Per-rule percentages; experiments have randomization units | Weights sum to exactly 100000 |

The bucketing kernel is good. The missing piece is the **release policy** on top: who moves the
percentage, when, and what stops it. That is a workflow problem more than an evaluation problem.

### 3.5 Flag types and variations

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Boolean / string / number / JSON | Yes, four types | Yes in the data model (2 to 100 variations, one JSON type, no `null`), but the dashboard creates booleans only |
| Multivariate | Yes | Yes |
| Variation size | 32 KB per string/JSON value | No explicit per-value limit (payload-size bound) |
| Flag templates | Release, kill switch, experiment, migration, custom | No templates; create is one shape |
| Migration flags | Two-to-six stages, cohorts, read/write semantics in server SDKs | No |
| Flag settings | Temporary/permanent, owner, tags, links, followers, client-side/mobile availability | Name, description, temporary, owner, tags; no availability controls |
| Flag status | New/Active/Launched/Inactive per environment, plus lifecycle stages | `lifecycle`: new, active, inactive, archived (from the last 7 days of usage); not per environment |

The dashboard creating booleans only is a product gap even though the API can do more.

### 3.6 Evaluation semantics and reasons

Our order is the same shape as LaunchDarkly's: off, prerequisites, individual targets, rules,
fallthrough. We return `OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH` and
`ERROR` with codes. LaunchDarkly returns similar reasons plus experiment/track metadata. We do not
have:

- evaluation events (every evaluation as an event, with context, variation and reason);
- `trackEvents` / `trackEventsFallthrough` on flags and rules;
- prerequisite details in events;
- migration read/write results;
- client-side per-context result streams;
- experiment assignment metadata.

**What we do better, and should keep:** one normative spec, shared vectors, two implementations, no
runtime dependence, synchronous evaluation, and explicit portability rules. LaunchDarkly's own docs
admit regex behaviour differs by SDK language. Our approach is a genuine differentiator for a
self-hosted product with multiple SDKs later.

---

## 4. Flag management and lifecycle

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Projects / environments | Multiple, unlimited on paid plans, two environments by default | Multiple, no environment edit/delete |
| Per-environment config | Yes | Yes, versioned compare-and-set |
| Optimistic concurrency | Yes | Yes, for flag config and segments |
| Archive / restore | Yes | Yes |
| Permanent delete | Yes, after archive | Yes, after archive, unless another flag needs it |
| Flag status / lifecycle | New, Active, Launched, Inactive; Ready for code removal / Ready to archive | New, Active, Inactive, Archived; no code-removal stage |
| Stale-flag detection | Yes, with lifecycle settings | No |
| Code references | `ld-find-code-refs`, extinction events, archive checks | No |
| Change history | Yes, per environment, with restore | Audit history is read-only; no restore |
| Clone flag | Yes | No |
| Compare/copy across environments | Yes | No |
| Bulk targeting | Yes | No |
| Mobile targeting | Yes | No |
| Flag links / following | Yes | No |
| Preview / test targeting for a context | Yes | No (call `evaluate` manually) |
| Flag triggers | Webhook/integration/adaptive | No |
| Scheduled changes | Flag and segment changes | No |
| Tags / views | Tags and views with payload filtering | Tags on flags; no views |
| Required comments/confirmation | Per environment | No |
| Approvals | Requestable; mandatory on Enterprise | No |

The lifecycle layer is where a flag service becomes usable by a team rather than by its author. We
have none of it beyond archive/restore.

---

## 5. Release workflow

LaunchDarkly's 2026 release surface:

- **Percentage rollouts** on a rule.
- **Progressive rollouts**: automatic percentage increase over time, one variation change per context.
- **Guarded rollouts**: progressive plus selected metrics, regression detection, notify or automatic
  rollback (Enterprise + Guardian add-on).
- **Audience-based releases**: lower-risk audiences first, checks between phases.
- **Release pipelines / policies / workflows**: standardise the path from merge to 100%.
- **Release Autopilot**: GitHub app reviews a PR, recommends a flag and release plan, applies both
  after one approval, then auto-releases, auto-cleans and auto-archives; guarded releases with
  auto-fix PRs.
- **Approvals**: flag, config, experiment and segment changes; required per environment or tag on
  Enterprise; 1 to 5 reviewers on Foundation.
- **Scheduled flag and segment changes**.
- **Flag triggers**: change a flag from a webhook, integration or metric threshold.
- **Health checks**, **release dashboard**, **feature monitoring**, **flag audiences**, **live events**,
  **user feedback** tied to a variation, and **applications/app versions** for organising what a
  release delivers.

ToggleSlash has **none of this**. The closest pieces are a toggle endpoint and a version check.

This is the largest product gap and the one most visible in a demo. It is also mostly workflow and
scheduling on top of data we already have, not new evaluation semantics.

---

## 6. SDKs and delivery

### 6.1 SDK breadth

LaunchDarkly publishes server-side SDKs for .NET, Apex, C++, Erlang, Go, Haskell, Java, Lua, Node.js,
PHP, Python, Ruby and Rust; client-side for JavaScript, React Web, Vue, Electron, Node.js, .NET,
Android, C++, Flutter, iOS, React Native and Roku; edge for Akamai, Cloudflare, Fastly and Vercel;
AI SDKs for .NET, Go, Node.js, Python and Ruby; and OpenFeature providers.

ToggleSlash has **one**: `packages/sdk`, TypeScript server-side. The C# evaluator is not a public
SDK. There is no browser SDK, no mobile SDK, no edge SDK. An OpenFeature provider (`packages/sdk-openfeature`) wraps the TypeScript SDK.

This is the distribution gap. A feature-flag service is bought by the language you already use. One
SDK means one kind of buyer.

### 6.2 Delivery and runtime behaviour

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Initial payload | Full ruleset (server-side) / evaluated values (client-side) | Full ruleset per environment |
| Updates | Streaming with deltas, polling fallback | SSE ETag signal + refetch; polling fallback |
| Bootstrap | Yes | No (the SDK starts empty and retries) |
| Offline mode | Yes | Indirectly: last ruleset held in memory |
| Persistent store | Redis, DynamoDB, Consul | No |
| Daemon mode / Relay Proxy | Yes, open source Go service | No; the server is the only source |
| Data saving mode | Yes, streaming deltas | No |
| Secure mode | Signed contexts for client-side | No |
| Private attributes | Yes | No (no context storage) |
| Hooks | Yes | No |
| OpenTelemetry | Yes | No |
| Big segments | Yes | No |
| Context kinds | Yes | No |
| Migration flags | Yes | No |
| Test data sources | Yes | No |
| Multiple environments (mobile) | Yes | No |
| Filtered payloads / views | Beta | No |
| Key types | SDK key, mobile key, client-side ID | Server key, client key |
| Client-side evaluation | Server evaluates and streams per-context values; ruleset never leaves the server | Client key may call `POST /sdk/v1/evaluate` per request; ruleset never leaves the server |
| Multi-process | Global service; Relay for local fan-out | **One process per database**; in-memory notifier and lock |

Our SSE design is clean and well tested, but it is a signal, not a delta stream, and it does not
cross processes. A real deployment with more than one server instance needs a bus, a shared lock, or
a relay.

### 6.3 What a browser SDK would require

The client key exists, and `POST /sdk/v1/evaluate` works from any origin. That is enough for a naive
browser SDK that calls the server per evaluation. It is not enough for a good one:

- per-context result caching and a change stream (otherwise every flag read is a network round trip);
- secure mode / signed contexts (otherwise anyone can evaluate as any context key);
- flag-level client availability controls (otherwise every flag is exposed to every client);
- private-attribute handling and event policy;
- a CDN/edge story for latency.

This is a design project, not a wrapper around the existing route.

---

## 7. Experimentation and metrics

LaunchDarkly's experimentation product includes A/B/n for flags and AI configs, A/A tests, Bayesian
and frequentist analysis, CUPED, sequential testing, sample-ratio-mismatch detection, confidence
intervals, multi-armed bandits, holdouts, mutually exclusive experiments, audience allocation,
randomization units, traffic reshuffling, warehouse-native experiments (Snowflake, Redshift,
Databricks, BigQuery, ClickHouse) and custom SQL metrics. Metrics can be clicks, page views, numeric,
conversion, custom events or groups, with measurement windows and winsorization.

ToggleSlash records **daily counts per flag, variation and environment**. It accepts no custom
events, stores no context, computes no metric and runs no experiment. There is no event pipeline at
all beyond `POST /sdk/v1/events`, which is a count sink.

This is not a small extension of the count table. It needs:

1. an event ingestion path (custom events, numeric values, context identifiers or a privacy-preserving
   assignment key);
2. a metric model and aggregation store;
3. experiment assignment (randomization unit, traffic allocation, exposure events);
4. a statistics layer (or a warehouse export and an external analysis tool);
5. a results UI.

If the goal is a flag service, this is a deliberate non-goal. If the goal is "LaunchDarkly
alternative", it is a second product.

---

## 8. Governance, security and compliance

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| User accounts | Yes | No |
| Login | Email/password, 2FA, SSO/SAML on Enterprise | One operator password, 12-hour signed session; no 2FA, no SSO |
| SCIM / IdP team sync | Enterprise | No |
| RBAC | Base roles, project roles, custom roles with resource/action policies, teams | One admin token; no roles |
| API tokens | Scoped, expiring, service tokens | One bootstrap admin token; SDK keys hashed |
| Audit log | Yes; unlimited history on Enterprise; export | Yes, every write in the same transaction, with before/after |
| Audit actor | Member/service token | Always `admin` |
| Required comments/confirmation | Per environment | No |
| Approvals | Yes; mandatory per environment/tag on Enterprise | No |
| Critical environments | Yes, with safeguards | No |
| Views / resource scoping | Yes | No |
| Compliance | SOC 2 Type II, ISO 27001, ISO 27701, DPF, FedRAMP Moderate | None claimed |
| Data residency | US, EU, federal | Self-hosted, so wherever you run it |

Our audit trail is genuinely good: atomic with the write, no secret leakage, tested for every write
route. It is also the only governance feature we have. Sign-in now keeps strangers out of the dashboard,
but everyone who holds the password is the same operator: without users, every audit row still says `admin`.

**What is left:** users, per-user sessions, roles, project scoping, scoped API tokens, SSO/SCIM, approval
policy, audit export/retention, and a limit on sign-in attempts inside the product.

---

## 9. Integrations and automation

LaunchDarkly: 80+ integrations, a REST API, a Terraform provider, a CLI, a hosted/local MCP server,
a GitHub app and Copilot/IDE extensions, Slack/Teams/Trello, CI/CD, observability and warehouse
integrations, data export, and edge/CDN integrations.

ToggleSlash: a committed OpenAPI document, 17 admin routes and 4 SDK routes, a CLI covering
projects, environments, flags and their targeting, segments and keys, and a dashboard. No
Terraform provider, no MCP server, no GitHub app, no chat integration, no data export, no webhooks.

The OpenAPI-first choice is the right foundation. A Terraform provider and an MCP server are
relatively cheap once the API is stable; the GitHub app and Release Autopilot are not.

---

## 10. Observability and AI

These are whole product lines, not checkbox features:

- **Observability**: session replay, error monitoring, logs, traces, OpenTelemetry metrics, product
  analytics, LLM observability, alerts, dashboards, services, data retention tiers.
- **AgentControl**: AI configs (prompts, model settings, tools, snippets), variations and targeting,
  playground, offline evaluations with datasets, online evaluations with judges, agent mode and agent
  graphs, optimization runs, AI metrics, AI Insights, guardrails/blocklists and PII redaction.
- **Vega**: an AI agent that investigates issues, manages flags, cleans up stale references and opens
  pull requests, including from Slack.

ToggleSlash has none of these, and should not pretend otherwise. Building any of them is a different company.
The only reason to touch this space is a narrow integration: allow a flag variation to carry an LLM
prompt/model config as JSON. That is already possible with a JSON flag; it is not AgentControl.

---

## 11. Operations and scale

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Storage | Managed, globally distributed | SQLite via EF Core |
| Horizontal scale | Yes | No: one process owns one file |
| Cross-process stream | Yes | No: in-memory notifier |
| Cross-process write lock | N/A | No: in-process `TargetingLock` only |
| Rate limiting | Yes (service-level) | Per key and per IP, in one process's memory |
| Quotas / abuse controls | Yes | Failed-auth lockout per IP, streams per key; no per-plan quotas |
| HA / failover | Yes, 99.99% claimed | None |
| Backups | Managed | Documented SQLite backup |
| Upgrades | Managed | Apply migrations at startup |
| Deployment | SaaS / Relay Proxy | Dockerfile + compose; image unpublished |
| Latency claim | <200 ms propagation | Local process; no published SLO |
| Regional / compliance | US, EU, federal | Self-hosted |

The single-process design was the right MVP trade. It becomes a liability the moment two instances
run behind a load balancer, and the code already documents why (`TargetingLock`, `RulesetNotifier`).

**What is left:** Postgres (or another shared store), a database-backed lock/version check, a
cross-process change bus (Postgres `LISTEN/NOTIFY`, Redis, NATS), a relay/cache tier, rate limits that cross processes,
and an HA deployment recipe.

---

## 12. What is left, prioritised

Sizes are rough: **S** days, **M** 1 to 3 weeks, **L** 1 to 2 months, **XL** a quarter or more, for one
experienced engineer. They are planning aids, not commitments.

### Phase 0: make it safe to expose (do first)

| Item | Why | Size |
| --- | --- | --- |
| Users, roles and project scoping | One admin token cannot serve a team; every audit row says `admin` | L |
| Scoped, expiring API tokens | Terraform/CI need credentials that are not the root token | M |
| Security headers, CORS review, body/time limits | Basic hardening before exposure | S |
| Audit actor from the authenticated principal | The audit trail is only useful with real actors | S |

### Phase 1: core flag parity (the credible small-team product)

| Item | Why | Size |
| --- | --- | --- |
| Multi-context and context kinds | Blocks org/device targeting, the most common real use case | L |
| Attribute references (`/address/city`) | Nested attributes are normal | M |
| Semantic-version and date operators | Two of the three missing operator families | S |
| Regex operator with a linear-time engine | The remaining operator gap; decide server-side only vs both | M |
| Browser SDK with caching + change stream | Without it the client key is half a product | L |
| Python, Go, Java and .NET SDKs | Distribution; pick by demand, share the vectors and a conformance suite | XL each |
| OpenFeature provider | Cheap interoperability win | M |
| Flag status, lifecycle and permanent delete | Users cannot tell what is safe to remove | M |
| Environment edit/delete | The API and dashboard stop at create | S |
| Bulk flag-state endpoint (FS-27) | The list makes one request per flag | S |
| CLI targeting and segment commands | The CLI stops at flags/keys | M |
| Dashboard full CRUD (restore, segment create/delete, env edit) | The UI is narrower than the API | M |

### Phase 2: release workflow (the LaunchDarkly-shaped demo)

| Item | Why | Size |
| --- | --- | --- |
| Approvals and required comments/confirmation | The governance feature buyers ask for first | L |
| Scheduled flag and segment changes | Cheap once a scheduler exists; high visible value | M |
| Progressive rollouts | Automatic percentage steps | M |
| Flag triggers / webhooks | Integrate with CI and incident tooling | M |
| Change-history restore | Audit becomes undo | M |
| Compare/copy flags across environments | Standard promotion workflow | M |
| Tags and views | Organise large flag sets; later payload filtering | M |
| Release policies / pipelines | Standardise the path to 100% | L |

### Phase 3: measurement

| Item | Why | Size |
| --- | --- | --- |
| Custom event ingestion | Needed for any metric beyond a count | L |
| Metric model and aggregation | Conversions, numerics, ratios | L |
| Experiment assignment and exposure | Randomization units, allocation, holdouts | XL |
| Statistics / warehouse export | Analysis without building a stats engine | L to XL |
| Guarded rollouts and auto-rollback | Depends on metrics + release workflow | XL |

### Phase 4: enterprise and scale

| Item | Why | Size |
| --- | --- | --- |
| Postgres support and migrations | Multi-instance, larger data, managed hosting | L |
| Cross-process lock and change bus | The documented single-process limits | M |
| Relay/cache tier | Local fan-out and lower latency | L |
| SSO/SAML, SCIM, custom RBAC, teams | Enterprise procurement | XL |
| Audit export and retention controls | Compliance | M |
| Terraform provider | Infrastructure-as-code buyers | M |
| Code references (`ld-find-code-refs` equivalent) | Stale-flag cleanup and archive safety | L |
| Large list-based and synced segments | Enterprise segment use cases | L |
| Mobile SDKs | Mobile is a large share of client-side use | XL |
| Edge/CDN delivery | Latency-sensitive client-side use | XL |

### Phase 5: platform expansion (recommended non-goals for now)

- Full observability (session replay, errors, logs, traces, metrics, alerts).
- AgentControl (AI configs, evals, judges, agent graphs).
- Release Autopilot (AI PR review, auto-flagging, auto-cleanup).
- 80+ integrations and marketplace billing.
- A global multi-region control plane.

Each is a company-sized product. The strategic question is whether ToggleSlash is "self-hosted
feature flags, done well" or "a LaunchDarkly clone". The first is achievable; the second is not, and
chasing it would dilute the parts that are already good.

---

## 13. Where we should deliberately differ

1. **Keep the normative evaluation spec and shared vectors.** It is the strongest engineering asset
   here. Every new SDK and operator must go through it. Do not let each SDK grow its own semantics.
2. **Keep "no context storage" as the default.** It is a privacy advantage and a simpler compliance
   story. If list-based segments or experiments arrive, make context storage explicit, scoped and
   opt-in, not a silent side effect of evaluation.
3. **Do not copy the regex free-for-all.** Either a linear-time engine on every implementation, or a
   documented server-side-only op. LaunchDarkly's own docs warn that regex behaviour varies by SDK.
4. **Keep the server-evaluates model for client keys.** It is safer than shipping rulesets. But pair
   it with caching and a change signal before calling it a client SDK.
5. **Keep audit writes atomic with the write.** That is better than a separate log pipeline and
   should survive any storage change.
6. **Do not build observability or AI until the flag product is complete.** A JSON flag can carry a
   prompt; that is enough for now.

## 14. What "done" means by audience

| Audience | What they need before ToggleSlash is a real option |
| --- | --- |
| A solo developer / small side project | Docker run, one server key, a server SDK, a dashboard they can reach safely (auth), and a browser SDK if the flags are client-side |
| A small team (5 to 20 engineers) | Users/roles, approvals or required comments, flag lifecycle, browser + 2 to 3 server SDKs, Postgres/HA story, audit export |
| A company comparing against LaunchDarkly | Everything above, plus progressive/guarded rollouts, experiments or a warehouse story, SSO/SCIM, Terraform, code references, 99.9%+ operations, and a support/compliance answer |
| A regulated / federal buyer | Self-hosted deployment, SSO, RBAC, audit retention, encryption/FIPS story, signed releases, an SBOM, and a security programme. Self-hosting helps; the paperwork does not exist yet |

---

## Appendix A: parity checklist

Legend: **Yes** = shipped; **Partial** = some of it; **No** = absent.

### Evaluation

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Boolean / string / number / JSON flags | Yes | Yes (API); boolean-only create UI |
| Multivariate variations | Yes | Yes |
| Individual targets | Yes | Yes |
| Targeting rules with clauses | Yes | Yes |
| Prerequisites | Yes | Yes |
| Off variation / default rule | Yes | Yes |
| Percentage rollouts | Yes | Yes (key-based) |
| Rollouts by attribute/context kind | Yes | No |
| Segments (rule/list) | Yes | Yes |
| Large list-based segments | Yes | No |
| Synced segments | Yes | No |
| Segment weights | Yes | Yes |
| Nested segments | No | No |
| Multi-context / context kinds | Yes | No |
| Attribute references | Yes | Yes |
| Regex operator | Yes | No (by design) |
| Semver operator | Yes | No |
| Date operators | Yes | No |
| Evaluation reasons | Yes | Yes |
| Track events on flags/rules | Yes | No |
| Migration flags | Yes | No |
| Experiments / holdouts / MAB | Yes | No |

### Management

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Projects / environments | Yes | Yes |
| Environment edit/delete | Yes | No |
| Per-environment config | Yes | Yes |
| Optimistic concurrency | Yes | Yes (flags and segments) |
| Archive / restore | Yes | Yes |
| Permanent delete | Yes | Yes |
| Flag status / lifecycle | Yes | Partial |
| Stale-flag detection | Yes | No |
| Code references | Yes | No |
| Change history restore | Yes | No |
| Clone / compare / copy | Yes | No |
| Bulk targeting | Yes | No |
| Mobile targeting | Yes | No |
| Flag links / following | Yes | No |
| Templates | Yes | No |
| Tags | Yes | Yes |
| Views / payload filtering | Yes | No |
| Scheduled changes | Yes | No |
| Approvals / comments / confirmation | Yes | No |
| Triggers / webhooks | Yes | No |
| Release pipelines / policies | Yes | No |
| Progressive / guarded rollouts | Yes | No |

### Platform

| Capability | LaunchDarkly | ToggleSlash |
| --- | --- | --- |
| Server SDKs | 13 languages | 1 (TypeScript) |
| Client-side SDKs | 12 | No |
| Mobile SDKs | 7 | No |
| Edge SDKs | 4 | No |
| AI SDKs | 5 | No |
| OpenFeature providers | Yes | Server TS only |
| Streaming updates | Yes | Yes (SSE, single process) |
| Polling fallback | Yes | Yes |
| Bootstrap / offline mode | Yes | No |
| Persistent store | Redis/DynamoDB/Consul | No |
| Relay proxy / daemon mode | Yes | No |
| Secure mode | Yes | No |
| Private attributes | Yes | No |
| Hooks / OpenTelemetry | Yes | No |
| Rate limiting | Yes | Yes, in memory |
| Multi-instance | Yes | No |
| Postgres/HA | Yes | No |
| Users / roles / SSO / SCIM | Yes | No |
| Audit log | Yes | Yes |
| Scoped API tokens | Yes | No (one admin token) |
| REST API | Yes | Yes |
| CLI | Yes | Partial |
| Terraform | Yes | No |
| MCP server | Yes | No |
| GitHub app | Yes | No |
| Chat integrations | Yes | No |
| Data export | Yes | No |
| Observability | Yes | No |
| AgentControl | Yes | No |
| Self-hosted / open source | Relay only | Yes (MIT) |

## Appendix B: sources

LaunchDarkly public documentation and pricing, read 2026-11-05:

- Platform overview: https://launchdarkly.com/platform/
- Docs index: https://launchdarkly.com/docs/llms.txt
- SDKs and supported features: https://launchdarkly.com/docs/sdk
- Choosing an SDK type (client/server/edge/AI, key types, private attributes): https://launchdarkly.com/docs/sdk/concepts/client-side-server-side
- Multi-contexts: https://launchdarkly.com/docs/home/flags/multi-contexts
- Targeting rules and operators: https://launchdarkly.com/docs/home/flags/target-rules
- Flag types: https://launchdarkly.com/docs/home/flags/types
- Segments: https://launchdarkly.com/docs/home/flags/segments-create
- Flag statuses and lifecycle: https://launchdarkly.com/docs/home/flags/flag-status
- Code references: https://launchdarkly.com/docs/home/flags/code-references
- Migration flags: https://launchdarkly.com/docs/home/flags/migration
- Release options: https://launchdarkly.com/docs/home/releases/releasing
- Approvals: https://launchdarkly.com/docs/home/releases/approvals
- Scheduled changes: https://launchdarkly.com/docs/home/releases/scheduled-changes
- Experimentation: https://launchdarkly.com/docs/home/experimentation
- Observability: https://launchdarkly.com/docs/home/observability
- AgentControl: https://launchdarkly.com/docs/home/agentcontrol
- Environments: https://launchdarkly.com/docs/home/account/environment
- Relay Proxy: https://launchdarkly.com/docs/sdk/relay-proxy
- Filtered payloads: https://launchdarkly.com/docs/sdk/concepts/payload-filtering
- Roles: https://launchdarkly.com/docs/home/account/roles
- Terraform provider: https://launchdarkly.com/docs/integrations/terraform
- Pricing and plan gating: https://launchdarkly.com/pricing/

ToggleSlash sources: `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/evaluation.md`,
`docs/api.md`, `docs/sdk.md`, `apps/server/openapi/v1.json` (22 paths: 17 admin, 4 SDK, plus
`/health`).
