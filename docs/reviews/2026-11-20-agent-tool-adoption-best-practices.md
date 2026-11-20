# Agent tool adoption: what the first-party sources actually say

**Date:** 2026-11-20 · **Scope:** how agents discover, choose, and ask an operator to install a developer tool or MCP server. Five questions, primary sources only, then checked against [`2026-11-20-agent-adoption-plan.md`](2026-11-20-agent-adoption-plan.md).

**Method:** read the current MCP specification and docs (latest revision `2026-07-28`, plus the superseded `2025-06-18` tools page and schema), the MCP Registry and Skills-extension docs, Anthropic's engineering posts ("Writing effective tools for agents", "Equipping agents for the real world with Agent Skills", "Demystifying evals for AI agents"), Anthropic's tool-definition reference, the Agent Skills specification, Claude Code docs (skills, permissions, plugin marketplaces, publishing, relevance, CLI hints), and OpenAI's function-calling guide. All URLs accessed 2026-11-20. Where a question has no first-party answer, it says so.

---

## 1. MCP tool design — what the sources say

**Names.** MCP `Tools` (rev. `2026-07-28`), **"Tool Names"**: 1–128 characters, case-sensitive, only `A-Z a-z 0-9 _ - .`, no spaces or commas, unique within a server. Uniqueness is per server, so aggregating clients "**SHOULD** implement a disambiguation strategy such as prefixing tool names with a server identifier." Anthropic's tool reference (**"Specifying client tools"**) is stricter for Claude API tools: `^[a-zA-Z0-9_-]{1,128}$`.

**Descriptions.** The spec only asks for "Human-readable description of functionality". Anthropic's `Define tools`, **"Best practices for tool definitions"**, calls descriptions "by far the most important factor in tool performance" and asks for what the tool does, when to use it *and when not to*, what each parameter means, and caveats — "Aim for at least 3–4 sentences". OpenAI's **"Best practices for defining functions"** says to use the system prompt to say when (and when not) to use a function, and to "Pass the intern test". Anthropic's **"Writing effective tools for agents"** (2025-09-11), **"Prompt-engineering your tool descriptions"**, frames it as onboarding a new hire and asks for unambiguous parameter names (`user_id`, not `user`).

**Schemas.** `inputSchema`/`outputSchema` are JSON Schema, defaulting to 2020-12. With an `outputSchema`, servers **MUST** return conforming structured results and clients **SHOULD** validate them (**"Output Schema"**). OpenAI recommends `strict: true`, which requires `additionalProperties: false` and every property required.

**Annotations.** `ToolAnnotations` = `title`, `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint` (`Schema Reference`, **"ToolAnnotations"**). They are hints only: "Clients should never make tool use decisions based on ToolAnnotations received from untrusted servers"; clients **MUST** treat them as untrusted unless the server is trusted (`Tools`, **"Tool"**). Anthropic points to annotations as the way to disclose open-world or destructive behaviour.

**Errors.** Two mechanisms: **protocol errors** (JSON-RPC: unknown tool, malformed request) and **tool execution errors** (`isError: true`) carrying "actionable feedback that language models can use to self-correct and retry" (`Tools`, **"Error Handling"**). Clients **SHOULD** surface execution errors to the model and **MAY** surface protocol errors, which are "less likely to result in successful recovery". Anthropic: "prompt-engineer your error responses to clearly communicate specific and actionable improvements."

**State.** MCP has no protocol-level session. **"Stateful Tools"** (non-normative) says to return an explicit handle from a creation tool, accept it later, state its retention policy in the creation tool's description, and return a tool execution error on an expired handle so the model can recover.

**How many.** The spec sets no number. OpenAI gives the only figure found: "**Aim for fewer than 20 functions available at the start of a turn** … though this is just a soft suggestion", with tool search for the rest. Anthropic: "More tools don't always lead to better outcomes"; consolidate chained operations; "Too many tools or overlapping tools can also distract agents." MCP **Client Best Practices** adds host-side **progressive discovery** — a `search_tools` meta-tool, switching once definitions reach "1%-5%" of the context window.

**What this means for Stow.** (1) Five tools with a `stow_` prefix is well inside every guideline; naming is already right. (2) Descriptions are one clause each ("Inspect the configured workspace without opening or claiming it.") — below the 3–4 sentence bar, and silent on when *not* to use and what comes next, so Phase 3 needs "what/when/when-not/returns/next", not "a sentence". (3) No `outputSchema`; MCP's own advice is that the fix sits with the server author. (4) No annotations; `inspect` tools are read-only, and whether `checkpoint_create` is idempotent depends on the caller reusing its request key — which is precisely what a static `idempotentHint` cannot express, so say so in the description instead. (5) Refusals belong in `isError: true` bodies, not JSON-RPC errors, if the model is to see Phase 0's `next`. (6) Keep the existing "persist a request key before calling" line — it is the stateful-handle pattern — and add the expiry rule.

---

## 2. Agent skills and instructions — what the sources say

**Triggering.** The Agent Skills spec (`agentskills.io/specification`, **"Frontmatter"**) requires `name` (max 64 chars, lowercase letters/digits/hyphens, matching the directory) and `description` (max 1024 chars) which "Should describe both what the skill does and when to use it" and "Should include specific keywords". Its good example names artefacts and ends "Use when working with PDF documents or when the user mentions PDFs, forms, or document extraction."; its bad example is "Helps with PDFs."

**Progressive disclosure.** Anthropic's **"Equipping agents for the real world with Agent Skills"** (2025-10-16), **"The anatomy of a skill"**: `name` + `description` are preloaded for every installed skill — "just enough information for Claude to know when each skill should be used" — the body loads on relevance, bundled files only as needed. The spec quantifies: metadata ~100 tokens, instructions "< 5000 tokens recommended", "Keep your main `SKILL.md` under 500 lines."

**Descriptions get truncated.** Claude Code **"Extend Claude with skills"**, **"Frontmatter reference"**: `description` is what Claude uses to decide, `when_to_use` is appended, and "the combined `description` and `when_to_use` text is truncated at 1,536 characters in the skill listing" — so "Put the key use case first". **"Skill not triggering"** starts with "Check the description includes keywords users would naturally say", and notes malformed frontmatter loads the body with empty metadata so the skill can never match.

**Evaluate first.** Anthropic's skills post, **"Developing and evaluating skills"**: start from observed failures on representative tasks, then iterate, and "Pay special attention to the `name` and `description` … Claude will use these when deciding whether to trigger the skill."

**Asking to install.** No first-party source tells a *skill* to instruct an agent to ask the operator to install software. What exists is host-side: a skill's `allowed-tools` only pre-approves tools "during the turn that invokes this skill", and for synced skills the grant goes "through the normal permission flow" (Claude Code, **"Frontmatter reference"**, **"How Claude Code handles the frontmatter of a synced skill"**); the spec marks the field experimental; and plugin relevance states "Claude Code never installs the plugin automatically. The user always confirms."

**What this means for Stow.** (1) Our description is ~760 characters: product summary first, then tasks, then a long "Triggers on …" list. It fits the 1024 limit but leads with *what it is* rather than the use case; the spec's example leads with verbs and artefacts. (2) If the trigger list moves to `when_to_use`, remember the 1,536-character combined cap — front-load, don't just shorten. (3) "The skill file is the storefront most agents read" is only true after install or in-repo; discovery is Q4. (4) The skill's "Asking for an install" section is our invention, not a documented pattern — and the host's approval UI, not skill text, is what gates installation. Test it as a hypothesis. (5) The body is long and linear; install card, refusal table, and compatibility contract are candidates for bundled reference files under the 500-line/5k-token guidance.

---

## 3. Tool-selection evaluation — what the sources say

Anthropic's **"Demystifying evals for AI agents"** (2026-01-09) is the only first-party document found with a full methodology.

**Vocabulary** (**"The structure of an evaluation"**): *task* = inputs + success criteria; *trial* = one attempt; *grader* = scoring logic; *transcript* = full record including tool calls; *outcome* = final environment state, which may differ from what the agent claims.

**Sample size** (Step 0, **"Collect tasks for the initial eval dataset"**): "20-50 simple tasks drawn from real failures is a great start"; teams that wait for hundreds delay building evals, because early on "each change to the system often has a clear, noticeable impact, and this large effect size means small sample sizes suffice."

**Task design** (Steps 1–3): start from manual checks and bug reports; require that "two domain experts would independently reach the same pass/fail verdict"; give each task a reference solution; build *balanced* sets — "Test both the cases where a behavior *should* occur and where it *shouldn't*. One-sided evals create one-sided optimization", citing their own web-search eval where the hard part was "undertriggering … or overtriggering".

**Grading** (**"Types of graders for agents"**, Step 5): code-based, model-based, human, each with stated weaknesses. "It's often better to grade what the agent produced, not the path it took", because agents find valid approaches designers did not anticipate; a `tool_calls` grader exists, but rigid ordering is "too brittle". Use partial credit. Calibrate LLM judges against humans and give them an "Unknown" escape.

**Non-determinism** (**"How to think about non-determinism in evaluations for agents"**): `pass@k` (≥1 success in k) vs `pass^k` (all k). At a 75% per-trial rate, three trials all pass only ~42% of the time.

**Harness/reporting** (Steps 4, 6, 7): clean environment per trial; read transcripts; trust no score until someone has read them; watch saturation and graders that reject valid work.

Anthropic's **"Writing effective tools for agents"**, **"Running an evaluation"**, adds: tasks from real use, strong tasks needing multiple tool calls, a verifiable outcome per prompt, optional expected-tool annotation (to test whether agents "grasp each tool's purpose") without overspecifying, a simple programmatic agent loop, and metrics for accuracy, runtime, tool calls, tokens, and tool errors — plus held-out sets. OpenAI's guide offers only "**Evaluate your performance** with different numbers of functions". No OpenAI tool-selection eval methodology was found.

**What this means for Stow.** (1) Ten tasks is below the stated range and, at a 50% target, has an interval wide enough to swallow the effect. (2) There are no negative controls, so the eval cannot catch an agent that pitches Stow constantly — the overtriggering failure named above. (3) The three-way outcome is a transcript judgement, so it needs an LLM rubric calibrated against a human reading the same traces, plus a reference trace per task. (4) The ask is stochastic: run several trials and report `pass^k`, not one run. (5) Read the traces before quoting a number; that is the sources' most repeated instruction. (6) Hold out tasks, or the tuning is fitted to the eval.

---

## 4. Distribution and discovery — what the sources say

**MCP Registry.** `registry/about`, **"The MCP Registry"**: "the official centralized metadata repository for publicly accessible MCP servers", in preview, storing `server.json` — reverse-DNS name (`io.github.user/server`), package or remote URL, execution instructions, description, capabilities. Namespace ownership is verified via GitHub, DNS, or HTTP. Private servers are unsupported, but a public install method (npm, public image, public remote) qualifies. Critically: "The MCP Registry is not intended to be directly consumed by host applications. Instead, host applications should consume other MCP registries, such as downstream marketplaces." The quickstart (**"Step 4: Create `server.json`"**) states "The Registry limits the `description` field to 100 characters", and requires `mcpName` in `package.json` matching the server name.

**Skills over MCP.** `extensions/skills/overview`, **"Skills"**: servers expose `SKILL.md` directories via `skills/list`, `skills/get`, `resources/read`, with SHA-256 manifests. Hosts **MUST NOT** retrieve files ahead of need, "Reading `SKILL.md` … does not itself activate a skill", and the host applies "any required user approval". Limits: **SHOULD NOT** exceed 512 files or 16 MiB per skill. SDK/host support "is still being implemented".

**Agent Skills** is an open standard at `agentskills.io`; Anthropic's post lists Claude.ai, Claude Code, the Agent SDK, and the Developer Platform as supported today.

**Claude Code plugins.** A marketplace is a repo with `.claude-plugin/marketplace.json` (`name`, `owner`, `plugins[]` with `name`, `source`); `claude plugin validate` checks it; users run `claude plugin marketplace add <owner>/<repo>` then `claude plugin install <plugin>@<marketplace>` (**"Create a marketplace"**). Anthropic's directory (`claude.ai/directory`) is a separate route requiring a paid claude.ai plan, and `claude-plugins-official` takes no portal submissions (**"Submit to Anthropic's directory"**).

**Relevance signals.** A marketplace entry may carry `relevance` with `cwd`, `cli`, `hosts`, `filesRead`, `manifestDeps` (**"Add relevance to a plugin entry"**). Matching is local; a match produces a spinner tip with the exact install command, a session-start notification, or a `/plugin` pin. But "In the terminal, users see no suggestions from a marketplace until an administrator allowlists it in managed settings", and frequency is capped (once every three sessions; the notification stops after two shows).

**CLI hints.** A CLI may print `<claude-code-hint v="1" type="plugin" value="name@marketplace" />` to stderr when `CLAUDECODE` is set; Claude Code strips it from model-visible output and shows a one-time **"Plugin recommendation"** dialog (**"Emit the hint"**, **"Preview what the user sees"**). Official-marketplace plugins only.

**What this means for Stow.** (1) Today's discovery surfaces — npm, repo `SKILL.md`, `site/agent.md`, a locally-run MCP server — all presuppose the agent has heard of Stow. (2) `server.json` is a one-file change and npm already satisfies the public-install rule, but the 100-character cap means it cannot hold the storefront copy. (3) The higher-leverage move is a plugin + marketplace in our own repo, bundling skill and MCP server so one install delivers both. (4) `relevance` fits us almost too well (`filesRead: **/docker-compose*.yml`, `cli: ["docker"]`, `manifestDeps` on AWS SDKs), but it only fires for admin-allowlisted marketplaces — teams, not strangers. (5) CLI hints are unavailable to us unless we land in an official Anthropic marketplace; do not plan around them. (6) Skills-over-MCP is the clean long-term answer ("install one thing, get the guidance"), but host support is incomplete — a later bet, not Phase 3.

---

## 5. Install and permission moments — what the sources say

**Human in the loop is a protocol expectation.** MCP `Tools`, **"User Interaction Model"**: "For trust & safety and security, there **SHOULD** always be a human in the loop with the ability to deny tool invocations"; applications SHOULD show exposed tools, indicate invocations visually, and "Present confirmation prompts to the user for operations". **"Security Considerations"** repeats: clients SHOULD prompt on sensitive operations and show inputs before calling.

**Installation specifically.** SEP-1024 (Final), **"Pre-Configuration Consent"**, requires any client offering one-click local server configuration to show "The exact command that will be executed, without truncation", all arguments and a danger warning, require explicit affirmative approval, offer cancel, and not proceed without consent. It binds the *client*, not the server or model.

**The harness owns permission.** Claude Code **"Configure permissions"**, **"Permission system"**: "Permission rules are enforced by Claude Code, not by the model. Instructions in your prompt or `CLAUDE.md` shape what Claude tries to do, but they don't change these rules." Read-only actions run unprompted; Bash, edits, web fetch and search prompt by default; deny is evaluated before ask, before allow.

**What a good moment looks like.** The relevance preview is one line — "Working with Terraform? Install the terraform-helpers plugin: `/plugin install terraform-helpers@your-marketplace`" — at most once every three sessions. The CLI-hint dialog names plugin, marketplace, description, and "the first word of the shell command Claude ran, so users can spot a mismatch"; no answer in 30 seconds counts as No; one prompt ever per plugin.

**Not found:** any first-party guidance on an agent recommending a commercial tool mid-task, negotiating an install, or persisting after refusal. Every documented pattern is host-mediated, frequency-limited, and user-confirmed.

**What this means for Stow.** (1) The agent can only *say* the sentence; it cannot install or grant itself permission through instructions. (2) Give the exact command verbatim, plus cost (one command, no daemon, no credentials, one directory to delete) — SEP-1024's shape and the plugin-suggestion shape agree. (3) Ask once, then drop it: the host patterns cap at once per three sessions and once ever per plugin because repetition reads as nagging. (4) State the fallback in the same breath, so a refusal leaves a plan. (5) Treat "the principal said no" as a first-class eval outcome; no source supports pushing past a refusal.

---

## Where this contradicts our current plan

Checked against `docs/reviews/2026-11-20-agent-adoption-plan.md`.

1. **The conversion eval is underpowered and one-sided.** Ten tasks sit below Anthropic's 20–50 starting range, and there are no control tasks — the source explicitly warns that one-sided evals create one-sided optimization. The plan cannot detect an agent that pitches Stow for everything. Add negatives, raise the count, run multiple trials, report `pass^k`.

2. **"The skill file is the storefront most agents read" is backwards on discovery.** A skill is read once installed or committed. Documented discovery runs through the MCP Registry (metadata for aggregators), plugin marketplaces, and host-side relevance suggestions — none of which Phase 3 builds. The skill is the closing surface, not the discovery surface.

3. **A registry listing cannot carry the message.** Phase 4 lists "MCP registry listings" as a proxy metric, but the registry caps `description` at 100 characters. If listed, its job is name plus install pointer; the copy must live in plugin/skill.

4. **The ask is treated as the primary mechanism; sources treat it as host-mediated and rate-limited.** Nothing found forbids a skill-authored ask, but nothing supports it either: every documented recommendation moment is host UI with hard caps and an explicit "never installs automatically". The plan should label the skill ask an untested hypothesis and measure whether it costs trust, not only whether it raises conversion.

5. **Phase 3's description work is under-specified.** "When to use, what is returned, what to do next" omits the explicit *when not to use* and the 3–4-sentence depth Anthropic asks for, plus `outputSchema` and annotations from MCP. "Descriptions match behaviour" is a consistency check, not a quality bar; current one-clause descriptions would fail the published guidance.

6. **Phase 1 puts the install card in five places but ships no plugin or marketplace artefact.** The documented agent-install surfaces are `claude plugin install`, `claude plugin marketplace add`, npm/`npx`, and the registry. A card in `README.md` and `site/agent.md` is read by humans and crawlers. A plugin manifest plus marketplace file is the missing artefact, and it is smaller than the Phase 2 handoff work.

7. **Minor.** "No telemetry, proxy metrics only" is defensible, but Anthropic treats monitoring and feedback as complementary, not replaceable. Say the eval is the only measurement, and expect it to be noisy at ten tasks.

### What the sources do not settle

- No first-party guidance exists on an agent asking a human to install software, so the plan's central mechanic has no documented prior art. That is its biggest unknown, not its safest assumption.
- No Google first-party Agent Skills equivalent was verified; the skills guidance here is Anthropic plus the open spec.
- The MCP Registry is in preview and may change or reset; the 100-character cap and `server.json` shape may move.
- MCP revisions move fast: the `2025-06-18` tools page read here is already superseded by `2026-07-28`, which adds tool-name rules, `server/discover`, and `InputRequiredResult`. Cite the revision when quoting.

---

## Sources

All accessed **2026-11-20**.

- MCP spec, Tools (2026-07-28) — https://modelcontextprotocol.io/specification/2026-07-28/server/tools
- MCP spec, Tools (2025-06-18, superseded) — https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- MCP Schema Reference — https://modelcontextprotocol.io/specification/2025-06-18/schema
- MCP spec, Discovery — https://modelcontextprotocol.io/specification/2026-07-28/server/discover
- MCP Client Best Practices — https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices
- MCP Registry, "The MCP Registry" — https://modelcontextprotocol.io/registry/about
- MCP Registry quickstart — https://modelcontextprotocol.io/registry/quickstart
- MCP Skills extension — https://modelcontextprotocol.io/extensions/skills/overview
- SEP-1024, local server installation — https://modelcontextprotocol.io/seps/1024-mcp-client-security-requirements-for-local-server-
- Anthropic, "Writing effective tools for agents" — https://www.anthropic.com/engineering/writing-tools-for-agents
- Anthropic, "Equipping agents for the real world with Agent Skills" — https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
- Anthropic, "Demystifying evals for AI agents" — https://www.anthropic.com/research/demystifying-evals-for-ai-agents
- Anthropic, Define tools — https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools
- Agent Skills specification — https://agentskills.io/specification
- Claude Code, Extend Claude with skills — https://code.claude.com/docs/en/skills
- Claude Code, Configure permissions — https://code.claude.com/docs/en/permissions
- Claude Code, Create a marketplace — https://code.claude.com/docs/en/plugin-marketplaces
- Claude Code, Publish and distribute a plugin — https://code.claude.com/docs/en/plugins/publish
- Claude Code, Recommend plugins for your org — https://code.claude.com/docs/en/plugins/relevance
- Claude Code, Recommend your plugin from your CLI — https://code.claude.com/docs/en/plugins/cli-hints
- OpenAI, Function calling — https://platform.openai.com/docs/guides/function-calling
